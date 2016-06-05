package protocol

const (
	StatusBadRequest = 400

	StatusNotImplemented = 501
	StatusBadGateway     = 502
)

var StatusText = map[int]string{
	StatusBadRequest: "Bad Request",

	StatusNotImplemented: "Not implemented",
	StatusBadGateway:     "Bad Gateway",
}

type Error struct {
	Status int
	Error error
}
