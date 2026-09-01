package utils

import "fmt"

func HumanSize(size int64) string {
	if size < 1024 {
		return fmt.Sprintf("%d B", size)
	}
	suffix := []string{"B", "KiB", "MiB", "GiB", "TiB"}
	s := float64(size)
	i := 0
	for s >= 1024 && i < len(suffix)-1 {
		s /= 1024
		i++
	}
	return fmt.Sprintf("%.1f %s", s, suffix[i])
}
