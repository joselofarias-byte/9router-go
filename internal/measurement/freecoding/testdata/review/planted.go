package task

// ClampPositive returns n when n is positive and 0 otherwise.
// The comparison below is planted backwards.
func ClampPositive(n int) int {
	if n > 0 {
		return 0
	}
	return n
}
