package app

import "example.com/bench/lib"

func BuildService() lib.Service {
	return lib.NewService("app")
}
