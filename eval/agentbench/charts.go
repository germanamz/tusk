package main

import (
	"fmt"
	"html"
	"math"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
)

// Chart is one figure of the report, rendered once per theme so a page can
// show the variant that matches its reader's color scheme.
type Chart struct {
	// Name is the file stem: the chart lands at charts/<name>-<theme>.svg.
	Name string
	// Alt describes the chart for the page that embeds it.
	Alt string
	// Variants maps a theme name to its SVG document.
	Variants map[string]string
}

// chartTheme is one color scheme, close to GitHub's light and dark ones.
// The arm colors are from the Okabe–Ito palette, which stays distinct
// under the common forms of color blindness.
type chartTheme struct {
	name       string
	background string
	text       string
	muted      string
	grid       string
	highlight  string
	arms       map[string]string
}

var chartThemes = []chartTheme{
	{
		name: "light", background: "#ffffff", text: "#1f2328", muted: "#59636e", grid: "#d1d9e0", highlight: "#fff8c5",
		arms: map[string]string{"grep": "#d55e00", "tusk": "#0072b2", "both": "#009e73"},
	},
	{
		name: "dark", background: "#0d1117", text: "#f0f6fc", muted: "#9198a1", grid: "#3d444d", highlight: "#2b2611",
		arms: map[string]string{"grep": "#e69f00", "tusk": "#56b4e9", "both": "#3fc59c"},
	},
}

const (
	chartWidth  = 760.0
	chartMargin = 16.0
	chartFont   = `-apple-system, BlinkMacSystemFont, "Segoe UI", Helvetica, Arial, sans-serif`
	// labelCharWidth approximates the width of one character of an 11px
	// label, enough to size the label column without measuring text.
	labelCharWidth = 6.3
)

// BuildCharts draws the report's charts: input tokens per question for
// every arm against the baseline, the spread of each cost metric by arm,
// and accuracy by arm and question kind.
func BuildCharts(records []RunRecord) []Chart {
	judging := slices.ContainsFunc(records, func(record RunRecord) bool { return record.JudgeCorrect != nil })
	rows := buildRows(records, judging)
	arms := presentArms(records)
	reps := 0

	for _, record := range records {
		reps = max(reps, record.Rep)
	}

	var charts []Chart

	for _, arm := range arms {
		if arm == baselineArm || !slices.Contains(arms, baselineArm) {
			continue
		}

		charts = append(charts, renderChart(
			"tokens-"+arm+"-vs-"+baselineArm,
			fmt.Sprintf("Input tokens per question, %s against %s, on a log scale", arm, baselineArm),
			func(theme chartTheme) string { return drawDumbbell(theme, rows, arm, reps) },
		))
	}

	charts = append(charts,
		renderChart("per-arm", "Spread of input tokens, tool calls, tool output and time per question, by arm",
			func(theme chartTheme) string { return drawSpread(theme, rows, arms) }),
		renderChart("accuracy", "Share of runs that cited the right section and that the judge marked correct, by arm and question kind",
			func(theme chartTheme) string { return drawAccuracy(theme, rows, arms, judging) }),
	)

	return charts
}

func renderChart(name string, alt string, draw func(chartTheme) string) Chart {
	chart := Chart{Name: name, Alt: alt, Variants: make(map[string]string)}

	for _, theme := range chartThemes {
		chart.Variants[theme.name] = draw(theme)
	}

	return chart
}

// WriteCharts writes every variant of every chart under dir/charts.
func WriteCharts(dir string, charts []Chart) error {
	chartsDir := filepath.Join(dir, "charts")

	if mkdirErr := os.MkdirAll(chartsDir, 0o755); mkdirErr != nil {
		return mkdirErr
	}

	for _, chart := range charts {
		for theme, document := range chart.Variants {
			path := filepath.Join(chartsDir, chart.Name+"-"+theme+".svg")

			if writeErr := os.WriteFile(path, []byte(document), 0o644); writeErr != nil {
				return writeErr
			}
		}
	}

	return nil
}

// ChartGallery is the markdown that embeds charts in a page at prefix,
// the path from the page to the directory holding charts/. GitHub picks
// the <source> whose media query matches the reader's theme.
func ChartGallery(charts []Chart, prefix string) string {
	var out strings.Builder

	for _, chart := range charts {
		fmt.Fprintf(&out, "<picture>\n  <source media=\"(prefers-color-scheme: dark)\" srcset=\"%[1]scharts/%[2]s-dark.svg\">\n  <img alt=\"%[3]s\" src=\"%[1]scharts/%[2]s-light.svg\">\n</picture>\n\n",
			prefix, chart.Name, html.EscapeString(chart.Alt))
	}

	return out.String()
}

// svgDoc accumulates the elements of one chart.
type svgDoc struct {
	theme  chartTheme
	height float64
	body   strings.Builder
}

func (doc *svgDoc) text(xPos float64, yPos float64, content string, attrs string) {
	fmt.Fprintf(&doc.body, `<text x="%.1f" y="%.1f" %s>%s</text>`+"\n", xPos, yPos, attrs, html.EscapeString(content))
}

func (doc *svgDoc) line(fromX float64, fromY float64, toX float64, toY float64, stroke string, width float64) {
	fmt.Fprintf(&doc.body, `<line x1="%.1f" y1="%.1f" x2="%.1f" y2="%.1f" stroke="%s" stroke-width="%.1f"/>`+"\n", fromX, fromY, toX, toY, stroke, width)
}

func (doc *svgDoc) rect(xPos float64, yPos float64, width float64, height float64, attrs string) {
	fmt.Fprintf(&doc.body, `<rect x="%.1f" y="%.1f" width="%.1f" height="%.1f" %s/>`+"\n", xPos, yPos, max(width, 0), max(height, 0), attrs)
}

func (doc *svgDoc) circle(centerX float64, centerY float64, radius float64, attrs string) {
	fmt.Fprintf(&doc.body, `<circle cx="%.1f" cy="%.1f" r="%.1f" %s/>`+"\n", centerX, centerY, radius, attrs)
}

// header draws the title and subtitle every chart opens with and returns
// the y position below them.
func (doc *svgDoc) header(title string, subtitle string) float64 {
	doc.text(chartMargin, 28, title, fmt.Sprintf(`font-size="15" font-weight="600" fill="%s"`, doc.theme.text))
	doc.text(chartMargin, 48, subtitle, fmt.Sprintf(`font-size="12" fill="%s"`, doc.theme.muted))

	return 64
}

// legend draws a dot and a name per arm from xPos along yPos.
func (doc *svgDoc) legend(xPos float64, yPos float64, arms []string) float64 {
	for _, arm := range arms {
		doc.circle(xPos+5, yPos-4, 5, fmt.Sprintf(`fill="%s"`, doc.theme.arms[arm]))
		doc.text(xPos+15, yPos, arm, fmt.Sprintf(`font-size="12" fill="%s"`, doc.theme.text))
		xPos += 28 + float64(len(arm))*7
	}

	return xPos
}

func (doc *svgDoc) String(alt string) string {
	return fmt.Sprintf(`<svg xmlns="http://www.w3.org/2000/svg" width="%[1]g" height="%[2]g" viewBox="0 0 %[1]g %[2]g" font-family='%[3]s' role="img" aria-label="%[4]s">
<title>%[4]s</title>
<rect width="100%%" height="100%%" rx="6" fill="%[5]s"/>
%[6]s</svg>
`, chartWidth, math.Ceil(doc.height), chartFont, html.EscapeString(alt), doc.theme.background, doc.body.String())
}

// axis maps values to pixels, on a linear scale from zero or on a log10
// scale, and lists the ticks to label.
type axis struct {
	low   float64
	high  float64
	start float64
	end   float64
	log   bool
	ticks []float64
}

func (scale axis) pixel(value float64) float64 {
	if scale.log {
		logValue, logLow, logHigh := math.Log10(max(value, scale.low)), math.Log10(scale.low), math.Log10(scale.high)

		return scale.start + (logValue-logLow)/(logHigh-logLow)*(scale.end-scale.start)
	}

	return scale.start + (value-scale.low)/(scale.high-scale.low)*(scale.end-scale.start)
}

// linearAxis runs from zero to a round number at or above top, with about
// four ticks.
func linearAxis(top float64, start float64, end float64) axis {
	if top <= 0 || math.IsNaN(top) {
		top = 1
	}

	step := niceAtLeast(top / 4)
	high := math.Ceil(top/step) * step

	var ticks []float64

	for tick := 0.0; tick <= high+step/2; tick += step {
		ticks = append(ticks, tick)
	}

	return axis{low: 0, high: high, start: start, end: end, ticks: ticks}
}

// logAxis spans from the 1-2-5 value at or below bottom to the one at or
// above top, with a tick at every 1-2-5 value between.
func logAxis(bottom float64, top float64, start float64, end float64) axis {
	low := niceAtMost(bottom)
	high := niceAtLeast(top)

	if high <= low {
		high = niceAtLeast(low * 1.01)
	}

	var ticks []float64

	for decade := math.Floor(math.Log10(low)); decade <= math.Ceil(math.Log10(high)); decade++ {
		for _, mantissa := range []float64{1, 2, 5} {
			tick := mantissa * math.Pow(10, decade)

			if tick >= low*0.999 && tick <= high*1.001 {
				ticks = append(ticks, tick)
			}
		}
	}

	return axis{low: low, high: high, start: start, end: end, log: true, ticks: ticks}
}

// niceAtLeast is the smallest 1, 2 or 5 times a power of ten that is at
// least value.
func niceAtLeast(value float64) float64 {
	decade := math.Pow(10, math.Floor(math.Log10(value)))

	for _, mantissa := range []float64{1, 2, 5, 10} {
		if mantissa*decade >= value*0.999 {
			return mantissa * decade
		}
	}

	return 10 * decade
}

// niceAtMost is the largest 1, 2 or 5 times a power of ten that is at most
// value.
func niceAtMost(value float64) float64 {
	decade := math.Pow(10, math.Floor(math.Log10(value)))

	for _, mantissa := range []float64{5, 2, 1} {
		if mantissa*decade <= value*1.001 {
			return mantissa * decade
		}
	}

	return decade
}

// drawAxis draws the tick labels below a plot and a grid line per tick
// from top to bottom.
func (doc *svgDoc) drawAxis(scale axis, top float64, bottom float64, format func(float64) string) {
	for _, tick := range scale.ticks {
		xPos := scale.pixel(tick)

		doc.line(xPos, top, xPos, bottom, doc.theme.grid, 1)
		doc.text(xPos, bottom+14, format(tick), fmt.Sprintf(`font-size="11" text-anchor="middle" fill="%s"`, doc.theme.muted))
	}
}

// compact writes a count the way an axis label wants it: 800, 1.5k, 20k.
func compact(value float64) string {
	switch {
	case value >= 1e6:
		return strconv.FormatFloat(value/1e6, 'f', -1, 64) + "M"
	case value >= 1e3:
		return strconv.FormatFloat(value/1e3, 'f', -1, 64) + "k"
	default:
		return strconv.FormatFloat(value, 'f', -1, 64)
	}
}

type dumbbellRow struct {
	label    string
	baseline float64
	other    float64
}

// drawDumbbell plots, for every question, the baseline's and arm's median
// input tokens on one line, sorted by their ratio, and shades the rows
// where the arm read more.
func drawDumbbell(theme chartTheme, rows []*questionRow, arm string, reps int) string {
	var points []dumbbellRow

	for _, row := range rows {
		baseCell, baseFound := row.cells[baselineArm]
		armCell, armFound := row.cells[arm]

		if !baseFound || !armFound || !(baseCell.totalInput > 0) || !(armCell.totalInput > 0) {
			continue
		}

		points = append(points, dumbbellRow{label: row.taskID, baseline: baseCell.totalInput, other: armCell.totalInput})
	}

	slices.SortStableFunc(points, func(left, right dumbbellRow) int {
		leftRatio, rightRatio := left.other/left.baseline, right.other/right.baseline

		switch {
		case leftRatio < rightRatio:
			return -1
		case leftRatio > rightRatio:
			return 1
		default:
			return strings.Compare(left.label, right.label)
		}
	})

	const rowHeight = 18.0

	longest := 0
	lowest, highest := math.Inf(1), math.Inf(-1)
	losses := 0

	for _, point := range points {
		longest = max(longest, len(point.label))
		lowest = min(lowest, point.baseline, point.other)
		highest = max(highest, point.baseline, point.other)

		if point.other > point.baseline {
			losses++
		}
	}

	if len(points) == 0 {
		lowest, highest = 1, 10
	}

	labelRight := chartMargin + float64(longest)*labelCharWidth
	scale := logAxis(lowest, highest, labelRight+16, chartWidth-chartMargin-64)

	doc := &svgDoc{theme: theme}
	top := doc.header(
		fmt.Sprintf("Input tokens per question: %s vs %s", arm, baselineArm),
		fmt.Sprintf("Median of %s per arm, log scale, sorted by %s ÷ %s. Shaded: %s read more (%d of %d).",
			plural(reps, "run"), arm, baselineArm, arm, losses, len(points)),
	)

	legendY := top + 12
	doc.legend(scale.start, legendY, []string{baselineArm, arm})
	doc.text(chartWidth-chartMargin, legendY, arm+" ÷ "+baselineArm, fmt.Sprintf(`font-size="11" text-anchor="end" fill="%s"`, theme.muted))

	rowsTop := legendY + 14
	rowsBottom := rowsTop + float64(len(points))*rowHeight

	for index, point := range points {
		if point.other > point.baseline {
			doc.rect(chartMargin/2, rowsTop+float64(index)*rowHeight, chartWidth-chartMargin, rowHeight, fmt.Sprintf(`fill="%s"`, theme.highlight))
		}
	}

	doc.drawAxis(scale, rowsTop, rowsBottom, compact)

	for index, point := range points {
		center := rowsTop + float64(index)*rowHeight + rowHeight/2
		baseX, otherX := scale.pixel(point.baseline), scale.pixel(point.other)

		doc.text(labelRight, center+4, point.label, fmt.Sprintf(`font-size="11" text-anchor="end" fill="%s"`, theme.text))
		doc.line(baseX, center, otherX, center, theme.muted, 2)
		doc.circle(baseX, center, 4.5, fmt.Sprintf(`fill="%s"`, theme.arms[baselineArm]))
		doc.circle(otherX, center, 4.5, fmt.Sprintf(`fill="%s"`, theme.arms[arm]))
		doc.text(chartWidth-chartMargin, center+4, fmt.Sprintf("%.2f×", point.other/point.baseline),
			fmt.Sprintf(`font-size="11" text-anchor="end" fill="%s" font-variant-numeric="tabular-nums"`, theme.text))
	}

	doc.text((scale.start+scale.end)/2, rowsBottom+32, "input tokens (log scale)", fmt.Sprintf(`font-size="11" text-anchor="middle" fill="%s"`, theme.muted))
	doc.height = rowsBottom + 48

	return doc.String(fmt.Sprintf("Input tokens per question, %s against %s", arm, baselineArm))
}

func plural(count int, noun string) string {
	if count == 1 {
		return "1 " + noun
	}

	return fmt.Sprintf("%d %ss", count, noun)
}

// drawSpread gives each cost metric a panel with a row per arm: one dot per
// question (its median run), a box from the 25th to the 75th percentile
// and a tick at the median.
func drawSpread(theme chartTheme, rows []*questionRow, arms []string) string {
	panels := []metric{
		{"Input tokens", costMetrics[0].value, compact},
		{"Tool calls", toolCallMetric.value, compact},
		{"Tool output", costMetrics[1].value, func(value float64) string { return compact(value/1000) + " KB" }},
		{"Time", costMetrics[2].value, func(value float64) string { return compact(value) + " s" }},
	}

	const (
		rowHeight  = 26.0
		labelRight = chartMargin + 44
		plotEnd    = chartWidth - chartMargin - 96
	)

	doc := &svgDoc{theme: theme}
	yPos := doc.header("Cost per question, by arm",
		"Each dot is one question's median run. Box: 25th to 75th percentile across questions; bar: median.") + 8

	for _, panel := range panels {
		top := 0.0

		for _, arm := range arms {
			for _, value := range collect(cellsFor(rows, arm), panel.value) {
				top = max(top, value)
			}
		}

		scale := linearAxis(top, labelRight+16, plotEnd)

		doc.text(chartMargin, yPos+14, panel.name, fmt.Sprintf(`font-size="13" font-weight="600" fill="%s"`, theme.text))

		rowsTop := yPos + 24
		rowsBottom := rowsTop + float64(len(arms))*rowHeight

		doc.drawAxis(scale, rowsTop, rowsBottom, panel.format)

		for armIndex, arm := range arms {
			values := collect(cellsFor(rows, arm), panel.value)
			center := rowsTop + float64(armIndex)*rowHeight + rowHeight/2
			color := theme.arms[arm]

			doc.text(labelRight, center+4, arm, fmt.Sprintf(`font-size="12" text-anchor="end" fill="%s"`, theme.text))

			if len(values) == 0 {
				continue
			}

			low, mid, high := quantile(values, 0.25), median(values), quantile(values, 0.75)

			doc.rect(scale.pixel(low), center-8, scale.pixel(high)-scale.pixel(low), 16,
				fmt.Sprintf(`fill="%s" fill-opacity="0.18" stroke="%s" stroke-width="1"`, color, color))

			for index, value := range values {
				// A fixed low-discrepancy jitter spreads the dots vertically
				// without randomness, so reruns draw the same chart.
				offset := (math.Mod(float64(index)*0.618034, 1) - 0.5) * 12

				doc.circle(scale.pixel(value), center+offset, 2.6, fmt.Sprintf(`fill="%s" fill-opacity="0.75"`, color))
			}

			doc.line(scale.pixel(mid), center-11, scale.pixel(mid), center+11, color, 3)
			doc.text(chartWidth-chartMargin, center+4, "median "+panel.format(roundForLabel(mid)),
				fmt.Sprintf(`font-size="11" text-anchor="end" fill="%s"`, theme.muted))
		}

		yPos = rowsBottom + 30
	}

	doc.height = yPos

	return doc.String("Cost per question, by arm")
}

// roundForLabel keeps a median label to about three significant digits.
func roundForLabel(value float64) float64 {
	if value == 0 || math.IsNaN(value) {
		return value
	}

	magnitude := math.Pow(10, math.Floor(math.Log10(math.Abs(value)))-2)

	return math.Round(value/magnitude) * magnitude
}

// kindGroup is the questions one block of the accuracy chart covers.
type kindGroup struct {
	name string
	rows []*questionRow
}

// drawAccuracy draws a bar per arm for every question kind, in one panel
// per accuracy measure.
func drawAccuracy(theme chartTheme, rows []*questionRow, arms []string, judging bool) string {
	measures := []metric{{"Section cited", func(summary *cell) float64 { return summary.sectionRate }, formatPercent}}

	if judging {
		measures = append(measures, metric{"Judged correct", func(summary *cell) float64 { return summary.correctRate }, formatPercent})
	}

	groups := []kindGroup{{"All questions", rows}}

	for _, kind := range []string{KindLexical, KindParaphrase} {
		var kindRows []*questionRow

		for _, row := range rows {
			if row.kind == kind {
				kindRows = append(kindRows, row)
			}
		}

		if len(kindRows) > 0 {
			groups = append(groups, kindGroup{fmt.Sprintf("%s%s (%d)", strings.ToUpper(kind[:1]), kind[1:], len(kindRows)), kindRows})
		}
	}

	const (
		barHeight = 14.0
		barGap    = 4.0
		panelGap  = 24.0
	)

	panelWidth := (chartWidth - 2*chartMargin - panelGap*float64(len(measures)-1)) / float64(len(measures))

	doc := &svgDoc{theme: theme}
	top := doc.header("Accuracy by arm and question kind",
		"Share of runs, averaged over questions. A failed run counts as a miss.")

	doc.legend(chartMargin, top+12, arms)

	bottom := 0.0

	for measureIndex, measure := range measures {
		left := chartMargin + float64(measureIndex)*(panelWidth+panelGap)
		barStart, barEnd := left+44, left+panelWidth-40
		yPos := top + 44

		doc.text(left, yPos, measure.name, fmt.Sprintf(`font-size="13" font-weight="600" fill="%s"`, theme.text))
		yPos += 10

		for _, group := range groups {
			doc.text(left, yPos+14, group.name, fmt.Sprintf(`font-size="12" fill="%s"`, theme.muted))
			yPos += 22

			for _, arm := range arms {
				rate := mean(collect(cellsFor(group.rows, arm), measure.value))
				width := 0.0

				if !math.IsNaN(rate) {
					width = rate * (barEnd - barStart)
				}

				doc.text(barStart-6, yPos+barHeight-3, arm, fmt.Sprintf(`font-size="11" text-anchor="end" fill="%s"`, theme.text))
				doc.rect(barStart, yPos, barEnd-barStart, barHeight, fmt.Sprintf(`fill="%s"`, theme.grid))
				doc.rect(barStart, yPos, width, barHeight, fmt.Sprintf(`fill="%s"`, theme.arms[arm]))
				doc.text(barEnd+6, yPos+barHeight-3, measure.format(rate), fmt.Sprintf(`font-size="11" fill="%s"`, theme.text))

				yPos += barHeight + barGap
			}

			yPos += 6
		}

		bottom = max(bottom, yPos)
	}

	doc.height = bottom + 12

	return doc.String("Accuracy by arm and question kind")
}
