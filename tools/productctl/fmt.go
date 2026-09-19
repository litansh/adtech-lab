package main

import "strconv"

func trimFloat(f float64) string { return strconv.FormatFloat(f, 'g', -1, 64) }
