package main

import (
	"math"
	"strconv"
)

func pct(f float64) string  { return trim(math.Round(f*1000)/10) + "%" }
func trim(f float64) string { return strconv.FormatFloat(f, 'g', -1, 64) }
