package claudeplugin

import (
	"fmt"
	"strings"
	"testing"
)

// itemSection renders count lines named "<prefix>-<n>", each width bytes
// long including its newline.
func itemSection(heading, prefix string, items, width int) Section {
	return Section{
		Heading: heading,
		Items:   items,
		Render: func(count int) (string, error) {
			var builder strings.Builder

			for index := range count {
				line := fmt.Sprintf("%s-%d", prefix, index)
				builder.WriteString(line + strings.Repeat(".", max(0, width-len(line)-1)) + "\n")
			}

			return builder.String(), nil
		},
	}
}

func wholeSection(heading, body string) Section {
	return Section{
		Heading: heading,
		Items:   1,
		Whole:   true,
		Render: func(int) (string, error) {
			return body, nil
		},
	}
}

func TestFit_UnderBudgetKeepsEverything(test *testing.T) {
	block, fitErr := Fit("orient\n", []Section{
		itemSection("Pinned", "pin", 2, 10),
		wholeSection("Aliases / standup", "standup rows\n"),
	}, 10_000)

	if fitErr != nil {
		test.Fatalf("Fit: %v", fitErr)
	}

	want := "orient\n\n## Pinned\npin-0....\npin-1....\n\n## Aliases / standup\nstandup rows\n"

	if block != want {
		test.Errorf("block =\n%q\nwant\n%q", block, want)
	}
}

func TestFit_DropsLowestPriorityFirst(test *testing.T) {
	sections := []Section{
		itemSection("Pinned", "pin", 3, 20),
		itemSection("Recent", "rec", 3, 20),
		wholeSection("Aliases / standup", strings.Repeat("x", 200)+"\n"),
	}

	full, _ := Fit("orient\n", sections, 1<<20)
	// Dropping the alias section frees over 200 bytes; the footer costs under
	// 100, so cutting 100 is enough to force exactly that drop.
	budget := len(full) - 100

	block, fitErr := Fit("orient\n", sections, budget)

	if fitErr != nil {
		test.Fatalf("Fit: %v", fitErr)
	}

	if strings.Contains(block, "## Aliases") {
		test.Errorf("alias section survived:\n%s", block)
	}

	if !strings.Contains(block, "rec-2") || !strings.Contains(block, "pin-2") {
		test.Errorf("pinned/recent were cut before the alias section was dropped:\n%s", block)
	}

	if !strings.HasSuffix(block, "call tusk_context for the full digest.\n") {
		test.Errorf("missing truncation footer:\n%s", block)
	}

	if len(block) > budget {
		test.Errorf("len(block) = %d, over budget %d", len(block), budget)
	}
}

func TestFit_TrimsWholeItemsFromTheEnd(test *testing.T) {
	sections := []Section{
		itemSection("Pinned", "pin", 4, 30),
		itemSection("Recent", "rec", 4, 30),
	}

	// Orientation + both headings + 4 pinned + 1 recent + footer.
	orientation := "orient\n"
	footer := "\n… digest truncated at 999 bytes; call tusk_context for the full digest.\n"
	budget := len(orientation) + len("\n## Pinned\n") + 4*30 + len("\n## Recent\n") + 30 + len(footer)

	block, fitErr := Fit(orientation, sections, budget)

	if fitErr != nil {
		test.Fatalf("Fit: %v", fitErr)
	}

	if !strings.Contains(block, "rec-0") || strings.Contains(block, "rec-1") {
		test.Errorf("want recent cut to one item:\n%s", block)
	}

	if !strings.Contains(block, "pin-3") {
		test.Errorf("pinned was cut while recent still had items:\n%s", block)
	}

	if len(block) > budget {
		test.Errorf("len(block) = %d, over budget %d", len(block), budget)
	}
}

func TestFit_NeverCutsOrientation(test *testing.T) {
	orientation := strings.Repeat("o", 100) + "\n"

	block, fitErr := Fit(orientation, []Section{itemSection("Pinned", "pin", 2, 10)}, 10)

	if fitErr != nil {
		test.Fatalf("Fit: %v", fitErr)
	}

	if !strings.HasPrefix(block, orientation) {
		test.Errorf("orientation was cut:\n%s", block)
	}

	if strings.Contains(block, "## Pinned") {
		test.Errorf("pinned survived a budget smaller than the orientation:\n%s", block)
	}

	if !strings.Contains(block, "digest truncated at 10 bytes") {
		test.Errorf("missing footer:\n%s", block)
	}
}

func TestFit_SkipsEmptySections(test *testing.T) {
	block, fitErr := Fit("orient\n", []Section{
		wholeSection("Aliases / empty", "  \n"),
		itemSection("Recent", "rec", 0, 10),
	}, 1000)

	if fitErr != nil {
		test.Fatalf("Fit: %v", fitErr)
	}

	if block != "orient\n" {
		test.Errorf("block = %q, want only the orientation", block)
	}
}

func TestFit_PropagatesRenderErrors(test *testing.T) {
	_, fitErr := Fit("orient\n", []Section{{
		Heading: "Recent",
		Items:   1,
		Render:  func(int) (string, error) { return "", fmt.Errorf("boom") },
	}}, 1000)

	if fitErr == nil || !strings.Contains(fitErr.Error(), "render Recent: boom") {
		test.Errorf("Fit error = %v, want render Recent: boom", fitErr)
	}
}
