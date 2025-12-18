package sagent

import (
	"crypto/rand"
	"fmt"
	"log"
	"sync"
	"time"
	"webcpy/sdriver"
	"webcpy/sdriver/dummy"
	"webcpy/streamAgent/webrtcHelper"

	"github.com/pion/webrtc/v4"
	"github.com/pion/webrtc/v4/pkg/media"
)

type Agent struct {
	sync.RWMutex
	VideoTrack *webrtc.TrackLocalStaticSample
	AudioTrack *webrtc.TrackLocalStaticSample
	driver     sdriver.SDriver
	config     ConnectionConfig
	// 用来接收前端的 RTCP 请求
	rtpSenderVideo *webrtc.RTPSender
	rtpSenderAudio *webrtc.RTPSender
	// chan
	videoCh   <-chan sdriver.AVBox
	audioCh   <-chan sdriver.AVBox
	controlCh chan sdriver.ControlEvent

	// 用于音视频推流的 PTS 记录
	lastVideoPTS time.Duration
	lastAudioPTS time.Duration
}

// ========================
// SAgent 主要负责从 sdriver 接收媒体流并通过 WebRTC 发送出去
// 同时处理来自客户端的控制命令并传递给 sdriver
// ========================
// 创建视频轨和音频轨，并初始化 Agent. 可以选择是否开启音视频同步.
func NewAgent(config ConnectionConfig) (*Agent, error) {
	sa := &Agent{
		config: config,
	}
	switch config.DeviceType {
	case DEVICE_TYPE_DUMMY:
		// 初始化 Dummy Driver
		dummyDriver, err := dummy.New(config.StreamCfg)
		if err != nil {
			log.Printf("Failed to initialize dummy driver: %v", err)
			return nil, err
		}
		sa.driver = dummyDriver
	case DEVICE_TYPE_ANDROID:
		// 初始化 Android Driver
		// scrcpyDriver :=
	default:
		log.Printf("Unsupported device type: %s", config.DeviceType)
		return nil, fmt.Errorf("unsupported device type: %s", config.DeviceType)
	}
	// sa.videoCh, sa.audioCh, sa.controlCh = sa.driver.GetReceivers()
	sa.videoCh, sa.audioCh, sa.controlCh = sa.driver.GetReceivers()
	videoCodec, audioCodec := sa.driver.CodecInfo()
	log.Printf("Driver codecs - Video: %s, Audio: %s", videoCodec, audioCodec)
	var videoMimeType, audioMimeType string
	switch videoCodec {
	case "h264":
		videoMimeType = webrtc.MimeTypeH264
	case "h265":
		videoMimeType = webrtc.MimeTypeH265
	case "av1":
		videoMimeType = webrtc.MimeTypeAV1
	default:
		log.Printf("Unsupported video codec: %s", videoCodec)
	}
	switch audioCodec {
	case "opus":
		audioMimeType = webrtc.MimeTypeOpus
	default:
		log.Printf("Unsupported audio codec: %s", audioCodec)
	}
	log.Printf("Creating tracks with MIME types - Video: %s, Audio: %s", videoMimeType, audioMimeType)
	streamID := generateStreamID()

	var videoStreamID, audioStreamID string
	if sa.config.AVSync {
		videoStreamID = streamID
		audioStreamID = streamID
	} else {
		videoStreamID = streamID + "_video"
		audioStreamID = streamID + "_audio"
	}

	var videoTrack, audioTrack *webrtc.TrackLocalStaticSample
	if videoMimeType != "" {
		// 创建视频轨
		videoTrack, _ = webrtc.NewTrackLocalStaticSample(
			webrtc.RTPCodecCapability{MimeType: videoMimeType},
			"video-track-id",
			videoStreamID, // <--- 使用不同的 StreamID 以取消强制同步
		)
	}
	if audioMimeType != "" {
		// 创建音频轨
		audioTrack, _ = webrtc.NewTrackLocalStaticSample(
			webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypeOpus}, // 假设音频是 Opus
			"audio-track-id",
			audioStreamID, // <--- 使用不同的 StreamID 以取消强制同步
		)
	}
	sa.VideoTrack = videoTrack
	sa.AudioTrack = audioTrack

	// go sa.StartBroadcaster()
	return sa, nil
}

func (sa *Agent) CreateWebRTCConnection(offer string) string {
	var finalSDP string
	finalSDP, sa.rtpSenderVideo, sa.rtpSenderAudio = webrtcHelper.HandleSDP(offer, sa.VideoTrack, sa.AudioTrack)
	return finalSDP
}

func (sa *Agent) Close() {

}

func (sa *Agent) GetCodecInfo() (string, string) {
	return sa.driver.CodecInfo()
}

func (sa *Agent) GetMediaMeta() sdriver.MediaMeta {
	return sa.driver.MediaMeta()
}

func (sa *Agent) Capabilities() sdriver.DriverCaps {
	return sa.driver.Capabilities()
}

func (sa *Agent) StartStreaming() {
	sa.driver.StartStreaming()
	go sa.StreamingVideo()
	go sa.StreamingAudio()
}

func (sa *Agent) PauseStreaming() {
}

func (sa *Agent) ResumeStreaming() {

}

func (sa *Agent) StreamingVideo() {
	// Default frame duration (e.g. 30fps) if delta is invalid
	defaultDuration := time.Millisecond * 33
	var baseTime time.Time

	for vBox := range sa.videoCh {
		// Initialize baseTime on first packet
		if baseTime.IsZero() {
			baseTime = time.Now()
		}

		var duration time.Duration
		if !vBox.IsConfig {
			if sa.lastVideoPTS == 0 {
				duration = defaultDuration
			} else {
				delta := vBox.PTS - sa.lastVideoPTS
				if delta <= 0 {
					duration = defaultDuration
				} else {
					duration = delta
				}
			}
			sa.lastVideoPTS = vBox.PTS
		} else {
			// Config 帧 (VPS/SPS/PPS) 不需要持续时间
			duration = 1 * time.Microsecond
		}

		// Use logical timestamp based on PTS instead of wall clock time
		timestamp := baseTime.Add(vBox.PTS)

		sample := media.Sample{
			Data:      vBox.Data,
			Duration:  duration,
			Timestamp: timestamp,
		}
		sa.VideoTrack.WriteSample(sample)
	}
}

func (sa *Agent) StreamingAudio() {
	for aBox := range sa.audioCh {
		var duration time.Duration

		// 1. 获取当前 PTS
		currentPTS := aBox.PTS

		// 2. 计算差值 (Duration)
		if sa.lastAudioPTS == 0 {
			// 第一帧：音频通常可以给一个标准值作为初始猜测
			// Opus 常见是 20ms
			duration = 20 * time.Millisecond
		} else {
			delta := currentPTS - sa.lastAudioPTS
			if delta <= 0 {
				// 音频的时间戳通常非常规律，如果出现 <=0，说明乱序严重
				// 给个极小值，或者直接丢弃这一帧（音频对乱序很敏感）
				duration = time.Microsecond
			} else {
				duration = delta
			}
		}

		// 3. 更新上一帧时间
		sa.lastAudioPTS = currentPTS

		// 4. 构造 Sample
		sample := media.Sample{
			Data:     aBox.Data,
			Duration: duration, // ✅ 让 Pion 根据真实的间隔来打 RTP 时间戳
			// Timestamp: time.Now(), // 可选，不需要用 UnixMicro 强转 PTS
		}

		sa.AudioTrack.WriteSample(sample)
	}
}

func (sa *Agent) SendControlEvent(event []byte) error {
	parsedEvent := sdriver.ControlEvent{}
	return sa.driver.SendControl(parsedEvent)
}

func generateStreamID() string {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return "scrcpy-stream"
	}
	return fmt.Sprintf("scrcpy-%x", b)
}
