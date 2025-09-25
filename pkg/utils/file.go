package utils

import "os"

func CreateDirIfNotExist(path string) error {
	err := os.MkdirAll(path, os.ModePerm)
	if err != nil {
		return err
	}
	return nil
}
