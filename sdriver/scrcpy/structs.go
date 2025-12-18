package scrcpy

type ScrcpyOptions struct {
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
	NewDisplay        string
	LogLevel          string
}

type ConnectOptions struct {
	DeviceSerial  string
	ReversePort   int
	ScrcpyOptions ScrcpyOptions
}

type ScrcpyVideoMeta struct {
	CodecID string
	Width   uint32
	Height  uint32
}
type ScrcpyAudioMeta struct {
	CodecID string
}

type ScrcpyFrameHeader struct {
	IsConfig   bool
	IsKeyFrame bool
	PTS        uint64
	Size       uint32
}

type ScrcpyFrame struct {
	Header  ScrcpyFrameHeader
	Payload []byte
}

type OpusHead struct {
	Magic      [8]byte
	Version    byte
	Channels   byte
	PreSkip    uint16
	SampleRate uint32
	OutputGain int16 // 注意：有符号
	Mapping    byte
}

type WebRTCFrame struct {
	Data      []byte
	Timestamp int64
	NotConfig bool
}

type WebRTCControlFrame struct {
	Data      []byte
	Timestamp int64
}
