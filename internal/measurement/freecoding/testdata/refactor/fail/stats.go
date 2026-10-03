package task

func Total(nums []int) int {
	sum := 0
	for _, n := range nums {
		sum += n
	}
	return sum
}

func Average(nums []int) int {
	sum := 0
	for _, n := range nums {
		sum += n
	}
	if len(nums) == 0 {
		return 0
	}
	return sum / len(nums)
}
