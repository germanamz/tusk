package graphview

import (
	"sort"
	"strconv"
)

// communitySeed is the fixed seed passed to graphcluster.Detect so the
// partition is deterministic across runs and processes. The graphcluster engine
// uses no randomization (it is fully order-based), so every seed value, including
// the default 0, produces the identical result; this constant is declared here
// rather than inlined so tests can verify it without depending on a magic literal.
const communitySeed = 1

// stableLabels maps a fresh integer partition to stable string labels,
// reusing the prior label held by the plurality of each new community's
// members. prev is nodeID -> previous label (may be nil/empty on first run).
// next is nodeID -> fresh community index from graphcluster.Detect. nodeIDs
// is the full node list in a deterministic order (snapshot order). Returns
// nodeID -> stable label for every id present in next.
//
// The returned mapping is a bijection over communities: every pair of nodes in
// the same community receives the same label, and every pair of nodes in
// distinct communities receives different labels.
//
// Label selection (collision-safe, fully deterministic):
//  1. Communities are processed in ascending order of their smallest member id.
//  2. For each community: tally prior-label votes; if there is a strict plurality
//     winner that has not yet been claimed, claim it.
//  3. Otherwise (no votes, tie, or winner already claimed): try the community's
//     smallest member id; if that is also claimed, iterate the community's members
//     in sorted order and take the first unclaimed id.
//  4. If every member id is already claimed (pathological but possible when a
//     previous community reused a label equal to a node id in this community),
//     derive a guaranteed-unique label: "<smallestMemberID>#<communityIndex>".
//
// Lock discipline: stableLabels is a pure function — it holds no locks and
// reads no server state. All server-side cross-snapshot state (prevCommunities,
// communityLabels, communityGen, communityGenSet) is guarded exclusively by
// communityMu, accessed only inside communityLabelsFor, which holds communityMu
// for its entire body. This ensures no data races: the four fields are never
// read or written outside that single critical section.
func stableLabels(prev map[string]string, next map[string]int, nodeIDs []string) map[string]string {
	// --- Step 1: group nodeIDs by their community index.
	// We visit nodeIDs in snapshot order so each community's member list is
	// order-stable and independent of map-iteration order.
	membersByComm := make(map[int][]string)

	for _, nodeID := range nodeIDs {
		commIdx, inNext := next[nodeID]

		if !inNext {
			continue
		}

		membersByComm[commIdx] = append(membersByComm[commIdx], nodeID)
	}

	// --- Step 2: for each community, pick a label.
	// Process communities in deterministic order: by the lexicographically
	// smallest member node id (which is the primary fallback label too).
	type commCandidate struct {
		index      int
		smallestID string
		// sortedMembers holds the community's member ids in sorted order,
		// populated lazily below for the fallback collision-resolution walk.
		sortedMembers []string
	}

	candidates := make([]commCandidate, 0, len(membersByComm))

	for commIdx, members := range membersByComm {
		smallest := members[0]

		for _, member := range members[1:] {
			if member < smallest {
				smallest = member
			}
		}

		candidates = append(candidates, commCandidate{index: commIdx, smallestID: smallest})
	}

	sort.Slice(candidates, func(left, right int) bool {
		return candidates[left].smallestID < candidates[right].smallestID
	})

	// Populate sortedMembers for each candidate now that the slice is stable.
	for i := range candidates {
		members := make([]string, len(membersByComm[candidates[i].index]))
		copy(members, membersByComm[candidates[i].index])
		sort.Strings(members)
		candidates[i].sortedMembers = members
	}

	// claimedLabels tracks labels already assigned in this pass to prevent
	// two communities from collapsing to the same label.
	claimedLabels := make(map[string]struct{}, len(candidates))

	// commLabel maps community index -> chosen stable label.
	commLabel := make(map[int]string, len(candidates))

	for ci, candidate := range candidates {
		members := membersByComm[candidate.index]

		// Tally votes from prev for this community's members.
		votes := make(map[string]int)

		for _, member := range members {
			if prevLabel, ok := prev[member]; ok && prevLabel != "" {
				votes[prevLabel]++
			}
		}

		// Find the plurality winner. On a tie (or no votes), the winner is
		// empty. We must not pick an arbitrary map winner, so we scan vote
		// entries in a deterministic order (sort by label) and track the best
		// seen so far.
		chosenLabel := ""
		bestVotes := 0
		isTied := false

		if len(votes) > 0 {
			sortedLabels := make([]string, 0, len(votes))

			for label := range votes {
				sortedLabels = append(sortedLabels, label)
			}

			sort.Strings(sortedLabels)

			for _, label := range sortedLabels {
				cnt := votes[label]

				if cnt > bestVotes {
					bestVotes = cnt
					chosenLabel = label
					isTied = false
				} else if cnt == bestVotes {
					isTied = true
				}
			}
		}

		// Use the chosen label only if it is a strict plurality winner and has
		// not already been claimed by an earlier community.
		if chosenLabel != "" && !isTied {
			if _, claimed := claimedLabels[chosenLabel]; !claimed {
				// Strict plurality winner that is still available — use it.
				claimedLabels[chosenLabel] = struct{}{}
				commLabel[candidate.index] = chosenLabel
				continue
			}
			// Winner already claimed — fall through to the collision-safe path.
		}

		// No votes, tie, or winner already claimed.
		// Walk the community's members in sorted order and take the first
		// unclaimed id. This is collision-safe because node ids are unique
		// globally and each node belongs to exactly one community; the only
		// way every member id could already be claimed is if a prior community
		// adopted a label that happens to equal a member id of this community
		// (possible when prev maps node ids as labels).
		pickedLabel := ""

		for _, memberID := range candidate.sortedMembers {
			if _, claimed := claimedLabels[memberID]; !claimed {
				pickedLabel = memberID
				break
			}
		}

		if pickedLabel == "" {
			// Pathological: every member id was already claimed as a label by
			// some prior community. Derive a guaranteed-unique label that is
			// still deterministic.
			pickedLabel = candidate.smallestID + "#" + strconv.Itoa(ci)
			// Ensure uniqueness even if the derived label somehow collides
			// (extremely unlikely but handle it defensively).
			for {
				if _, claimed := claimedLabels[pickedLabel]; !claimed {
					break
				}

				pickedLabel += "#"
			}
		}

		claimedLabels[pickedLabel] = struct{}{}
		commLabel[candidate.index] = pickedLabel
	}

	// --- Step 3: build the output map: nodeID -> stable label.
	out := make(map[string]string, len(next))

	for _, nodeID := range nodeIDs {
		commIdx, inNext := next[nodeID]

		if !inNext {
			continue
		}

		out[nodeID] = commLabel[commIdx]
	}

	return out
}
