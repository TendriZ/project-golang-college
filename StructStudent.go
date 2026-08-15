package main

import (
	"fmt"
)

type Student struct {
	ID string 
	Name string
	Grade float64
	isActive bool
}

//menggunakan value receiver karena tidak mengubah data apapun
func (s *Student) GetInfo() string {
	return fmt.Sprintf (
		"ID: %s, Name:%s, Grade: %.2f, Active: %t", 
		s.ID, 
		s.Name, 
		s.Grade,  
		s.isActive,
	)
}

//menggunakan pointer receiver karena mengubah data struct
func (s *Student) UpdateGrade(grade float64) {
	s.Grade = grade
}

//menggunakan pointer receiver karena mengubah data struct
func (s *Student) Activate() {
	s.isActive = true
}

//menggunakan pointer receiver karena mengubah data struct
func (s *Student) Deactivate() {
	s.isActive = false
}

func main() {
	student := Student{
		ID : "434241056",
		Name : "Muhammad Raka Razzani",
		Grade: 4.0,
		isActive: true,
	}

	fmt.Println("Data awal student:")
	fmt.Println(student.GetInfo())

	//Mengubah grade (update)
	student.UpdateGrade(5.0)
	
	//Mengubah status ke inactive
	student.Deactivate()

	//Menampilkan kembali hasil pembaruan data
	fmt.Println("Data student setelah pembaruan:")
	fmt.Println(student.GetInfo())
}