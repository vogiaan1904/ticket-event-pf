package service

func (s implReservationService) sumQuantities(m map[int64]int) int {
	total := 0
	for _, qty := range m {
		total += qty
	}
	return total
}
