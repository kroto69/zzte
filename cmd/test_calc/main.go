package main

import (
	"fmt"
	"olt-monitor/internal/snmp"
)

func main() {
	shelf := 1
	slot := 1
	port := 2

	// Formula: (shelf * 2^25) + (slot * 2^16) + (port * 2^8)

	// Manual calculation
	shelfPart := shelf << 25
	slotPart := slot << 16
	portPart := port << 8

	total := shelfPart + slotPart + portPart

	fmt.Printf("Shelf: %d -> %d (binary: %b)\n", shelf, shelfPart, shelfPart)
	fmt.Printf("Slot:  %d -> %d (binary: %b)\n", slot, slotPart, slotPart)
	fmt.Printf("Port:  %d -> %d (binary: %b)\n", port, portPart, portPart)
	fmt.Printf("Total ifIndex: %d\n", total)

	// Use library function
	libCalc := snmp.CalculateIfIndex(shelf, slot, port)
	fmt.Printf("Library Calculation: %d\n", libCalc)
}
