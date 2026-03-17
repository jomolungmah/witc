package sample

type Server struct {
	Addr string
}

func (s *Server) Start() error {
	return nil
}

func (s *Server) Stop() error {
	return nil
}

type Handler interface {
	ServeHTTP(w, r interface{})
}

func Helper(x int) string {
	return ""
}
