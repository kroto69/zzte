package main

import "fmt"

func main() {
	// User data
	// Slot 1 (likely PON 1, Board 1) = 268501248
	// Slot 2 (likely PON 2, Board 1) = 268501504

	target1 := 268501248
	target2 := 268501504

	// New Formula Hypothesis:
	// Base 0x10000000 (1 << 28)
	// Board << 16
	// (PON - 1) << 8

	board := 1

	// Test PON 1
	pon1 := 1
	calc1 := (1 << 28) + (board << 16) + ((pon1 - 1) << 8)

	// Test PON 2
	pon2 := 2
	calc2 := (1 << 28) + (board << 16) + ((pon2 - 1) << 8)

	fmt.Printf("PON 1 Target: %d, Calculated: %d, Match: %v\n", target1, calc1, target1 == calc1)
	fmt.Printf("PON 2 Target: %d, Calculated: %d, Match: %v\n", target2, calc2, target2 == calc2)

	fmt.Printf("Hex 1: %X\n", calc1)
	fmt.Printf("Hex 2: %X\n", calc2)
}
