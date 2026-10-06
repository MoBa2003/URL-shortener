package main

import (
	"fmt"
	"reflect"
	"urlshortener/internal/shortener"
)

func main() {
	fmt.Println("                  fijjwifj efjkewp     jfpwjfwp               pfrjewpjf  fef            ")
	fmt.Println(shortener.NormalizeURL("        https://www.youtube.com/watch?v=qEo9z_KNWEk&list=WL&index=3               "))
	x := "salam"
	for _, val := range x {
		fmt.Println(reflect.TypeOf(val))
	}

	fmt.Println(shortener.GenerateCode(6))

	info1 := info{name: "mammad", lname: "sharaf", data: 13}
	info1.changename("reza")
	fmt.Println(info1)
}

type info struct {
	name  string
	lname string
	data  any
}

func (sample *info) changename(newname string) {
	(*sample).name = newname
}
