package main

import "path/filepath"

func filepath_Glob(pattern string) ([]string, error) {
	return filepath.Glob(pattern)
}

func filepath_Match(pattern, name string) (bool, error) {
	return filepath.Match(pattern, name)
}
