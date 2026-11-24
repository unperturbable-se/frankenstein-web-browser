package main

import "fmt"

func main() {
	base := "https://en.wikipedia.org/wiki/Go_(programming_language)"
	body:=http1_1(base)
	fmt.Println(body)
}