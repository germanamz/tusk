package htmlunit

// headingLevel maps an html heading tag name to its 1..6 level. The
// boolean is false for non-heading tags.
func headingLevel(tag string) (int, bool) {
	switch tag {
	case "h1":
		return 1, true
	case "h2":
		return 2, true
	case "h3":
		return 3, true
	case "h4":
		return 4, true
	case "h5":
		return 5, true
	case "h6":
		return 6, true
	default:
		return 0, false
	}
}
