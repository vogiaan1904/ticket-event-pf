package util

import (
	"math/rand"
	"reflect"
)

// ExistsInSlide check if item exists in slice
func Contains[T comparable](arr []T, item T) bool {
	for _, a := range arr {
		if reflect.DeepEqual(a, item) {
			return true
		}
	}
	return false
}

// UniqueSlide remove duplicate items in slice
func RemoveDuplicates(input []string) []string {
	if len(input) == 0 {
		return nil
	}

	seen := make(map[string]bool, len(input))
	result := make([]string, 0, len(input))

	for _, item := range input {
		if !seen[item] {
			seen[item] = true
			result = append(result, item)
		}
	}

	return result
}

func Intersect[T comparable](a, b []T) []T {
	m := make(map[T]bool)
	for _, item := range a {
		m[item] = true
	}

	var result []T
	for _, item := range b {
		if m[item] {
			result = append(result, item)
		}
	}
	return result
}

func RandomString(length int) string {
	letters := "ABCDEFGHIJKLMNOPQRSTUVWXYZ"
	result := make([]byte, length)
	for i := 0; i < length; i++ {
		result[i] = letters[rand.Intn(len(letters))]
	}
	return string(result)
}
