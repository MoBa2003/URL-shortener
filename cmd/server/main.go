package main

import (
	"fmt"
	"math"
)

func main() {
	// x := 10
	fmt.Println(calculatesum(25))
	fmt.Println(containsNearbyDuplicate([]int{1, 2, 3, 4, 2}, 3))

}

func calculatesum(n int) (sum int) {
	for n > 0 {
		r := n % 10
		sum += r * r
		n /= 10
	}
	return
}

func containsNearbyDuplicate(nums []int, k int) bool {
	mymap := map[int]int{}

	for idx, val := range nums {
        if lastidx,exists := mymap[val];exists && math.Abs(float64(idx)-float64(lastidx)) <= float64(k){
         return true
        }
        mymap[val] = idx
	}
	return false

}
