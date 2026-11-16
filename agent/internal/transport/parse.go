package transport

import (
	"fmt"
	"strconv"
	"strings"
)

// Component names can contain dashes, so the line is split from the right.
// Hosts older than 5.2.1er2 list the type before the variant; that gets swapped back.
func ParseServiceLine(zone, line string, known func(Service) bool) (Service, error) {
	line = strings.TrimSpace(line)
	if !strings.HasPrefix(line, zone+"-") {
		return Service{}, fmt.Errorf("line does not start with zone %q", zone)
	}
	parts := strings.Split(line[len(zone)+1:], "-")
	if len(parts) < 3 {
		return Service{}, fmt.Errorf("too few fields")
	}
	n := len(parts)
	variant, typ := parts[n-2], parts[n-1]
	if isDigits(typ) && strings.HasPrefix(variant, "SVC_") {
		variant, typ = typ, variant
	}
	head := parts[:n-2]
	mk := func(i int) Service {
		return Service{
			Zone:             zone,
			Component:        strings.Join(head[:i], "-"),
			LogicalComponent: strings.Join(head[i:], "-"),
			Variant:          variant,
			Type:             typ,
		}
	}
	if len(head) == 1 {
		return mk(1), nil
	}
	def := mk(len(head) - 1)
	if known == nil {
		return def, nil
	}
	for i := len(head) - 1; i >= 1; i-- {
		if c := mk(i); known(c) {
			return c, nil
		}
	}
	return def, nil
}

func isDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func ParseBool(v string) (val, ok bool) {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "1", "1.0", "true", "on", "yes":
		return true, true
	case "0", "0.0", "false", "off", "no":
		return false, true
	}
	return false, false
}

func ParseLevel(v string) (float64, bool) {
	v = strings.TrimSpace(v)
	if i := strings.IndexByte(v, ','); i >= 0 {
		v = strings.TrimSpace(v[:i])
	}
	v = strings.TrimRight(v, "%FCKfck° ")
	if v == "" || v == "X" || v == "-" {
		return 0, false
	}
	f, err := strconv.ParseFloat(v, 64)
	if err != nil || f < 0 {
		return 0, false
	}
	return f, true
}
