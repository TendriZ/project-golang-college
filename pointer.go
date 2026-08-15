package main

import (
	"fmt"
)

func main() {
	// pass by pointer
	num1 := 1000
	num2 := 2000
	fmt.Println("Sebelum swap: num1:", num1, " num2:", num2)
	swap(&num1, &num2)
	fmt.Println("Setelah swap: num1:", num1, " num2:", num2)

	//pass by pointer dengan slice
	slice:= []string{"Raka", "Adil", "Habib"}
	fmt.Println("Sebelum update:", slice)
	updateSlice(&slice, "Bagas")
	fmt.Println("Updated slice:", slice)
}

func swap(a, b *int) { 
	*a, *b = *b, *a
}
func updateSlice(s *[]string, newItem string) { 
	*s = append(*s, newItem)
}
