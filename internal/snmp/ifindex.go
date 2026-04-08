package snmp

// CalculateIfIndex calculates the ifIndex for a PON port
// Formula: (1 << 28) + (slot << 16) + (port << 8)
// This matches observed C320 ifIndex values (e.g. 268501248 for board 1 pon 1)
func CalculateIfIndex(shelf, slot, port int) int {
	// Base 0x10000000 (1 << 28) = 268435456
	return (1 << 28) + (slot << 16) + (port << 8)
}

// ParseIfIndex extracts shelf, slot, and port from an ifIndex
func ParseIfIndex(ifIndex int) (shelf, slot, port int) {
	shelf = (ifIndex >> 28) & 0x0F // Upper nibble
	slot = (ifIndex >> 16) & 0xFF  // 8 bits for slot
	port = (ifIndex >> 8) & 0xFF   // 8 bits for port
	return
}

// CalculateIfIndexDefault calculates ifIndex with default shelf=1
func CalculateIfIndexDefault(board, pon int) int {
	return CalculateIfIndex(1, board, pon)
}
