package audio

import (
	"context"
	"io"
	"time"

	"github.com/dharmab/skyeye/pkg/simpleradio/types"
	"github.com/dharmab/skyeye/pkg/simpleradio/voice"
	"github.com/rs/zerolog/log"
)

// maxRxGap is a duration after which the receiver will assume the end of a transmission if no packets are received.
// TODO make this configurable.
const maxRxGap = 300 * time.Millisecond

// receiveUDP listens for incoming UDP packets and routes them to the appropriate channel.
func (c *audioClient) receiveUDP(ctx context.Context, pingCh chan<- []byte, voiceCh chan<- []byte) {
	for {
		if ctx.Err() != nil {
			if ctx.Err() == context.Canceled {
				log.Info().Msg("stopping SRS packet receiver due to context cancellation")
			} else {
				log.Error().Err(ctx.Err()).Msg("stopping packet receiver due to context error")
			}
			return
		}

		udpPacketBuf := make([]byte, 1500)
		n, err := c.connection.Read(udpPacketBuf)
		udpPacket := make([]byte, n)
		copy(udpPacket, udpPacketBuf[0:n])

		switch {
		case err == io.EOF:
			log.Error().Err(err).Msg("UDP connection closed")
		case err != nil:
			log.Error().Err(err).Msg("UDP connection read error")
		case n == 0:
			log.Warn().Err(err).Msg("0 bytes read from UDP connection")
		case n < types.GUIDLength:
			log.Debug().Int("bytes", n).Msg("UDP packet smaller than expected")
		case n == types.GUIDLength:
			// Ping packet
			pingCh <- udpPacket
		case n > types.GUIDLength:
			// Voice packet
			deadline := time.Now().Add(maxRxGap)
			c.lastRx.deadline = deadline
			voiceCh <- udpPacket
		}
	}
}

// receivePings listens for incoming UDP ping packets and logs them at DEBUG level.
func (c *audioClient) receivePings(ctx context.Context, in <-chan []byte) {
	for {
		select {
		case b := <-in:
			n := len(b)
			if n < types.GUIDLength {
				log.Debug().Int("bytes", n).Msg("received UDP ping smaller than expected")
			} else if n > types.GUIDLength {
				log.Debug().Int("bytes", n).Msg("received UDP ping larger than expected")
			} else {
				log.Trace().Str("GUID", string(b[0:types.GUIDLength])).Msg("received UDP ping")
			}
		case <-ctx.Done():
			log.Info().Msg("stopping SRS ping receiver due to context cancellation")
			return
		}
	}
}

// receiveVoice listens for incoming UDP voice packets, decodes them into VoicePacket structs, and routes them to the out channel for audio decoding.
func (c *audioClient) receiveVoice(ctx context.Context, in <-chan []byte, out chan<- []voice.VoicePacket) {
	// buf is a buffer of voice packets which are collected until the end of a transmission is detected.
	buf := make([]voice.VoicePacket, 0)
	// t is a ticker which triggers the check for the end of a transmission.
	t := time.NewTicker(frameLength)
	for {
		select {
		case b := <-in:
			vp, err := decodeVoicePacket(b)
			if err != nil {
				log.Debug().Err(err).Msg("failed to decode voice packet")
				continue
			}
			if vp == nil {
				log.Warn().Msg("nil pointer returned from decodeVoicePacket")
				continue
			}

			// isNewPacket is true if the packet is the first packet of a new transmission. This is the case if c.lastRx's fields are zero values.
			isNewPacket := c.lastRx.origin == "" && c.lastRx.packetNumber == 0
			// isSameOrigin is true if the packet's origin GUID matches the last received packet's origin GUID.
			isSameOrigin := c.lastRx.origin == types.GUID(vp.OriginGUID)
			// isNewerPacket is true if the packet's packet number is greater than the last received packet's packet number.
			isNewerPacket := vp.PacketID > uint64(c.lastRx.packetNumber)

			// isSameFrequency is true if the packet's frequencies contain a frequency which matches the client's radio's frequency, modulation, and encryption settings.
			var isSameFrequency bool
			for _, f := range vp.Frequencies {
				radio := types.Radio{
					Frequency:     f.Frequency,
					Modulation:    types.Modulation(f.Modulation),
					IsEncrypted:   f.Encryption != 0,
					EncryptionKey: f.Encryption,
				}
				if c.radio.IsSameFrequency(radio) {
					isSameFrequency = true
					break
				}
			}

			// isMatchingPacket is true if the packet is either:
			//   - the first packet of a new transmission
			//   - a newer packet from the same origin and with matching radio frequencies as the last received packet
			isMatchingPacket := isSameFrequency && (isNewPacket || (isNewerPacket && isSameOrigin))

			// If the packet fits, buffer it and update the lastRx state.
			if isMatchingPacket {
				buf = append(buf, *vp)
				c.updateLastRX(vp)
			}
		case <-t.C:
			// Check if there is enough in the buffer and that we've consumed all queued packets. Then check if we've passed the receive deadline.
			// If so, we have a tranmission ready to publish for audio decoding.
			// 25 packets * 40ms = 1s which is the minimum whisper can transcribe anyway.
			if len(buf) > 25 && len(in) == 0 && time.Now().After(c.lastRx.deadline) {
				log.Trace().Int("bufferLength", len(buf)).Uint64("lastPacketID", c.lastRx.packetNumber).Str("lastOrigin", string(c.lastRx.origin)).Msg("passed receive deadline with packets in buffer")
				audio := make([]voice.VoicePacket, len(buf))
				copy(audio, buf)
				out <- audio
				// Reset receiver state
				buf = make([]voice.VoicePacket, 0)
				c.resetLastRx()
			}
		case <-ctx.Done():
			log.Info().Msg("stopping SRS audio receiver due to context cancellation")
			return
		}
	}
}

// updateLastRX updates the lastRx state with the origin and packet number of the given voice packet.
func (c *audioClient) updateLastRX(vp *voice.VoicePacket) {
	c.lastRx.origin = types.GUID(vp.OriginGUID)
	c.lastRx.packetNumber = vp.PacketID
}

// resetLastRx resets the lastRx state to zero values.
func (c *audioClient) resetLastRx() {
	c.lastRx.packetNumber = 0
	c.lastRx.origin = ""
}
