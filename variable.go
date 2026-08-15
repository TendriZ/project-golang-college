package main 

import (
	"fmt"
)

func main() {
	//Deklarasi 5 variabel dengan tipe data berbeda
	var iniString string = "Learn Golang"
	var iniInt int = 10
	var iniFloat float64 = 19.07
	var iniBoolean bool = true
	var filmFavorit []string = []string{"Avengers", "Spiderman", "Batman"}

	fmt.Println("String:", iniString)
	fmt.Println("Integer:", iniInt)
	fmt.Println("Float:", iniFloat)
	fmt.Println("Boolean:", iniBoolean)
	fmt.Println("Slice:", filmFavorit)

	//Mapping data mahasiswa
	mahasiswa := make(map[string]float64) //dalam bentuk persen

	//di sinin kita tambahkan datanya
	mahasiswa["Raka"] = 85.6
	mahasiswa["Meliana"] = 90.0
	mahasiswa["Tendriz"] = 88.5

	fmt.Println("Kehadiran Mahasiswa:")
	for nama, persen := range mahasiswa {
		fmt.Printf("%s: %.2f%%\n", nama, persen)
	}

	//membaca data dan mengecek keberadaan key
	kehadiran,exist := mahasiswa["Raka"]
	if exist {
		fmt.Printf("Kehadiran Raka: %.2f%%\n", kehadiran)
	} else {
		fmt.Println("Data tidak exist")
	}

	// menghapus data
	delete(mahasiswa, "Raka")
	fmt.Println("Sesudah dihapus menjadi:", mahasiswa)

	//menelusuri isi map
	fmt.Println("Isi map mahasiswa:")
	for key, value := range mahasiswa {
		fmt.Printf("%s: %.2f%%\n", key, value)
	}
}

