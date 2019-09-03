package model

type Req struct {
	Op  uint32 `json:"op"`
	Msg string `json:"msg"`
}

type Rsp struct {
	Code uint32 `json:"code"`
	Msg  string `json:"msg"`
}
