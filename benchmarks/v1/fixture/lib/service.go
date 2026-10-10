package lib

type Service struct {
	Name string
}

func NewService(name string) Service {
	return Service{Name: name}
}
