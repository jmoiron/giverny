package kanban

import (
	"strconv"
	"strings"
)

var rawLabelPalette = []string{
	"#556270", "#4ECDC4", "#C7F464", "#FF6B6B", "#C44D58",
	"#7A1E91", "#3BBFBC", "#CFDB35", "#B16052", "#8A2A3D",
	"#69D2E7", "#A7DBD8", "#E0E4CC", "#F38630", "#FA6900",
	"#F590FF", "#BFA6F6", "#80B3FC", "#40BFFA", "#00CCF8",
	"#D92645", "#FA502A", "#DFDB35", "#44B4CC", "#2C228B",
}

var labelPalette = dedupePalette(rawLabelPalette, 28)

func nextLabelColor(used []string) string {
	if len(labelPalette) == 0 {
		return "#888888"
	}
	usedSet := make(map[string]struct{}, len(used))
	for _, color := range used {
		usedSet[strings.ToUpper(strings.TrimSpace(color))] = struct{}{}
	}
	for _, color := range labelPalette {
		if _, ok := usedSet[strings.ToUpper(color)]; !ok {
			return color
		}
	}
	return labelPalette[len(used)%len(labelPalette)]
}

func dedupePalette(colors []string, threshold float64) []string {
	result := make([]string, 0, len(colors))
	seen := make([]rgb, 0, len(colors))
	for _, color := range colors {
		normalized, parsed, ok := parseHexColor(color)
		if !ok {
			continue
		}
		tooClose := false
		for _, existing := range seen {
			if colorDistance(parsed, existing) < threshold {
				tooClose = true
				break
			}
		}
		if tooClose {
			continue
		}
		result = append(result, normalized)
		seen = append(seen, parsed)
	}
	return result
}

type rgb struct {
	r float64
	g float64
	b float64
}

func parseHexColor(color string) (string, rgb, bool) {
	color = strings.TrimSpace(strings.TrimPrefix(color, "#"))
	if len(color) != 6 {
		return "", rgb{}, false
	}
	rv, err := strconv.ParseUint(color[0:2], 16, 8)
	if err != nil {
		return "", rgb{}, false
	}
	gv, err := strconv.ParseUint(color[2:4], 16, 8)
	if err != nil {
		return "", rgb{}, false
	}
	bv, err := strconv.ParseUint(color[4:6], 16, 8)
	if err != nil {
		return "", rgb{}, false
	}
	return "#" + strings.ToUpper(color), rgb{
		r: float64(rv),
		g: float64(gv),
		b: float64(bv),
	}, true
}

func colorDistance(a, b rgb) float64 {
	dr := a.r - b.r
	dg := a.g - b.g
	db := a.b - b.b
	return sqrt(dr*dr + dg*dg + db*db)
}

func sqrt(v float64) float64 {
	if v <= 0 {
		return 0
	}
	z := v
	for i := 0; i < 8; i++ {
		z -= (z*z - v) / (2 * z)
	}
	return z
}
