package sagent

import (
	"time"

	"github.com/pion/webrtc/v4/pkg/media"
)

func (sa *Agent) StreamingVideo() {

	const defaultDuration = time.Millisecond * 16

	var firstPTS time.Duration = -1

	for vBox := range sa.videoCh {
		if firstPTS == -1 {
			firstPTS = vBox.PTS
		}

		// 计算当前帧相对于第一帧过了多久
		// 这样可以确保 ptsOffset 从 0 开始增长
		ptsOffset := vBox.PTS - firstPTS

		// Timestamp = Agent启动时间 + 视频流逝的时间
		timestamp := sa.baseTime.Add(ptsOffset)

		var duration time.Duration

		if vBox.IsConfig {
			// Config 帧 (SPS/PPS) 不应该占据时间轴
			duration = 0
		} else {
			// 如果是第一帧 VCL
			if sa.lastVideoPTS == 0 {
				duration = defaultDuration
			} else {
				// 计算与上一帧的时间差
				delta := (vBox.PTS - sa.lastVideoPTS)

				if delta <= 0 {
					duration = defaultDuration
				} else {
					duration = delta
				}
			}
			sa.lastVideoPTS = vBox.PTS
		}

		sample := media.Sample{
			Data:      vBox.Data,
			Duration:  duration,
			Timestamp: timestamp,
		}

		// 错误处理是必要的，防止 Track 关闭后 panic
		if err := sa.VideoTrack.WriteSample(sample); err != nil {
			// log.Println("WriteSample error:", err)
			return
		}
	}
}

func (sa *Agent) StreamingAudio() {
	// 音频通常是非常规律的，Opus 默认帧长通常是 20ms
	const defaultDuration = 20 * time.Millisecond

	var firstPTS time.Duration = -1

	for aBox := range sa.audioCh {
		if firstPTS == -1 {
			firstPTS = aBox.PTS
		}

		ptsOffset := aBox.PTS - firstPTS
		timestamp := sa.baseTime.Add(ptsOffset)

		// var duration time.Duration

		// if aBox.IsConfig {
		// 	duration = 0
		// } else {
		// 	if sa.lastAudioPTS == 0 {
		// 		duration = defaultDuration
		// 	} else {
		// 		delta := aBox.PTS - sa.lastAudioPTS
		// 		if delta <= 0 {
		// 			duration = defaultDuration
		// 		} else {
		// 			duration = delta
		// 		}
		// 	}
		// 	sa.lastAudioPTS = aBox.PTS
		// }

		// log.Printf("Audio PTS: %v, Timestamp: %v, Duration: %v\n", aBox.PTS, timestamp, duration)
		sample := media.Sample{
			Data:      aBox.Data,
			Duration:  defaultDuration,
			Timestamp: timestamp,
		}

		if err := sa.AudioTrack.WriteSample(sample); err != nil {
			// log.Printf("Audio WriteSample err: %v\n", err)
			return
		}
	}
}
