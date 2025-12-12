package scrcpy

type ScrcpyParams struct {
	CLASSPATH         string
	Version           string
	SCID              string
	MaxSize           string
	MaxFPS            string
	VideoBitRate      string
	Control           string
	Audio             string
	VideoCodec        string
	VideoCodecOptions string
	LogLevel          string
}

type ScrcpyVideoMeta struct {
	CodecID string
	Width   uint32
	Height  uint32
}

type ScrcpyVideoFrameHeader struct {
	IsConfig   bool
	IsKeyFrame bool
	PTS        uint64
	Size       uint32
}

type ScrcpyVideoFrame struct {
	Header  ScrcpyVideoFrameHeader
	Payload []byte
}

type ScrcpyAudioMeta struct {
	CodecID string
}

type ScrcpyAudioFrame struct{}

type ScrcpyControlFrame struct{}

type WebRTCVideoFrame struct {
	Data      []byte
	Timestamp int64
}

type WebRTCAudioFrame struct {
	Data      []byte
	Timestamp int64
}
type WebRTCControlFrame struct {
	Data      []byte
	Timestamp int64
}
