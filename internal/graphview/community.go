package graphview

import "sort"

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
	// smallest member node id (which is the fallback label too).
	type commCandidate struct {
		index      int
		smallestID string
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

	// claimedLabels tracks labels already assigned in this pass to prevent
	// two communities from collapsing to the same label.
	claimedLabels := make(map[string]struct{}, len(candidates))

	// commLabel maps community index -> chosen stable label.
	commLabel := make(map[int]string, len(candidates))

	for _, candidate := range candidates {
		members := membersByComm[candidate.index]
		fallbackLabel := candidate.smallestID

		// Tally votes from prev for this community's members.
		votes := make(map[string]int)

		for _, member := range members {
			if prevLabel, ok := prev[member]; ok && prevLabel != "" {
				votes[prevLabel]++
			}
		}

		// Find the plurality winner. On a tie (or no votes), fall back to the
		// smallest-member-id label. We must not pick an arbitrary map winner,
		// so we scan vote entries in a deterministic order (sort by label) and
		// track the best seen so far.
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
			if _, claimed := claimedLabels[chosenLabel]; claimed {
				// Collision: another community already took this label; fall back.
				chosenLabel = fallbackLabel
			}
		} else {
			// No votes, zero votes, or a tie: use the deterministic fallback.
			chosenLabel = fallbackLabel
		}

		// If even the fallback is claimed (e.g. a previous community was assigned
		// this node's id as its label), we still use it for the current community
		// because the fallback is derived from this community's own smallest member
		// and is therefore unique per community by construction (each node id is
		// in exactly one community in a partition). Two communities can share the
		// fallback only if the same node id is their smallest member, which cannot
		// happen in a valid partition.
		claimedLabels[chosenLabel] = struct{}{}
		commLabel[candidate.index] = chosenLabel
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
