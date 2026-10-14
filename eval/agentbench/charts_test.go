package main

import (
	"encoding/xml"
	"errors"
	"io"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
)

func TestBuildCharts(test *testing.T) {
	charts := BuildCharts(syntheticRecords())

	var names []string

	for _, chart := range charts {
		names = append(names, chart.Name)

		for _, theme := range chartThemes {
			document, found := chart.Variants[theme.name]

			if !found {
				test.Fatalf("%s has no %s variant", chart.Name, theme.name)
			}

			assertWellFormed(test, chart.Name+"-"+theme.name, document)
		}
	}

	if want := []string{"tokens-tusk-vs-grep", "per-arm", "accuracy"}; !slices.Equal(names, want) {
		test.Errorf("charts = %v, want %v", names, want)
	}

	dumbbell := charts[0].Variants["light"]

	for _, fragment := range []string{">alpha<", ">gamma<", "Shaded: tusk read more (1 of 3)", "1.50×", "0.50×"} {
		if !strings.Contains(dumbbell, fragment) {
			test.Errorf("dumbbell lacks %q", fragment)
		}
	}

	// Sorted by ratio: beta and alpha (0.50×, by name) before gamma (1.50×).
	order := regexp.MustCompile(`>(alpha|beta|gamma)<`).FindAllStringSubmatch(dumbbell, -1)

	if len(order) != 3 || order[0][1] != "alpha" || order[1][1] != "beta" || order[2][1] != "gamma" {
		test.Errorf("dumbbell rows out of order: %v", order)
	}

	if accuracy := charts[2].Variants["dark"]; !strings.Contains(accuracy, "Judged correct") || !strings.Contains(accuracy, "Paraphrase (1)") {
		test.Errorf("accuracy chart lacks its judged panel or kind groups")
	}
}

func TestBuildChartsWithoutBaseline(test *testing.T) {
	records := slices.DeleteFunc(syntheticRecords(), func(record RunRecord) bool { return record.Arm == baselineArm })

	charts := BuildCharts(records)

	if len(charts) != 2 || charts[0].Name != "per-arm" {
		test.Errorf("charts without a baseline = %d, want per-arm and accuracy only", len(charts))
	}
}

func assertWellFormed(harness testing.TB, name string, document string) {
	harness.Helper()

	decoder := xml.NewDecoder(strings.NewReader(document))

	for {
		_, tokenErr := decoder.Token()

		if errors.Is(tokenErr, io.EOF) {
			return
		}

		if tokenErr != nil {
			harness.Fatalf("%s is not well-formed XML: %v", name, tokenErr)
		}
	}
}

func TestLogAxis(test *testing.T) {
	scale := logAxis(9000, 61000, 0, 100)

	if scale.low != 5000 || scale.high != 100000 {
		test.Errorf("domain = %v to %v, want 5000 to 100000", scale.low, scale.high)
	}

	if want := []float64{5000, 10000, 20000, 50000, 100000}; !slices.Equal(scale.ticks, want) {
		test.Errorf("ticks = %v, want %v", scale.ticks, want)
	}

	if math.Abs(scale.pixel(5000)) > 1e-9 || math.Abs(scale.pixel(100000)-100) > 1e-9 {
		test.Errorf("ends map to %v and %v", scale.pixel(5000), scale.pixel(100000))
	}

	if single := logAxis(10000, 10000, 0, 100); single.high <= single.low {
		test.Errorf("a single value gives an empty domain: %+v", single)
	}
}

func TestLinearAxis(test *testing.T) {
	if scale := linearAxis(7, 0, 100); !slices.Equal(scale.ticks, []float64{0, 2, 4, 6, 8}) {
		test.Errorf("ticks for 7 = %v", scale.ticks)
	}

	if scale := linearAxis(0, 0, 100); scale.high <= 0 {
		test.Errorf("all-zero values give an empty domain: %+v", scale)
	}
}

func TestWriteChartsAndGallery(test *testing.T) {
	dir := test.TempDir()
	charts := BuildCharts(syntheticRecords())

	if writeErr := WriteCharts(dir, charts); writeErr != nil {
		test.Fatal(writeErr)
	}

	gallery := ChartGallery(charts, "")

	for _, match := range regexp.MustCompile(`(?:srcset|src)="([^"]+)"`).FindAllStringSubmatch(gallery, -1) {
		if _, statErr := os.Stat(filepath.Join(dir, match[1])); statErr != nil {
			test.Errorf("gallery links %s, which wasn't written: %v", match[1], statErr)
		}
	}

	if strings.Count(gallery, "<picture>") != len(charts) {
		test.Errorf("gallery has %d pictures for %d charts", strings.Count(gallery, "<picture>"), len(charts))
	}
}
