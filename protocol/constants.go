package protocol

const MethodConnect = "CONNECT"

const (
	StatusOK = 200

	StatusBadRequest = 400
	StatusForbidden  = 403

	StatusNotImplemented = 501
	StatusBadGateway     = 502
)

var StatusText = map[int]string{
	StatusOK: "OK",

	StatusBadRequest: "Bad Request",
	StatusForbidden:  "Forbidden",

	StatusNotImplemented: "Not implemented",
	StatusBadGateway:     "Bad Gateway",
}

type Error struct {
	Status int
	Error  error
}
