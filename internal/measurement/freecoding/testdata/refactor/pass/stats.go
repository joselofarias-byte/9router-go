package task

func Total(nums []int) int {
	sum := 0
	for _, n := range nums {
		sum += n
	}
	return sum
}

func Average(nums []int) int {
	if len(nums) == 0 {
		return 0
	}
	return Total(nums) / len(nums)
}
