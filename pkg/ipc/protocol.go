package ipc

type Request struct {
	Method string         `json:"method"`
	Params map[string]any `json:"params"`
}

type Response struct {
	OK     bool   `json:"ok"`
	Error  string `json:"error,omitempty"`
	Result any    `json:"result,omitempty"`
}
