package client

import (
	"errors"
	"fmt"
	"log"
	"net"
	"os/exec"
	"time"

	"github.com/asticode/go-astiav"
	"github.com/pion/webrtc/v3"
	"github.com/pion/webrtc/v3/pkg/media"
)

func openCameraFeed(peerConnection *webrtc.PeerConnection, videoTrack *webrtc.TrackLocalStaticSample) error {
    // Handle incoming tracks
    peerConnection.OnTrack(func(track *webrtc.TrackRemote, receiver *webrtc.RTPReceiver) {
        fmt.Println("Track received:", track.Kind())
        fmt.Println("Track Codec:", track.Codec())
        fmt.Println("Track Codec MimeType:", track.Codec().MimeType)
        go func() {
            for {
                // Read frames from the track
                _, _, err := track.ReadRTP()
                if err != nil {
                    log.Println("Error reading RTP:", err)
                    return
                }

                // Handle the frames as needed and render into a video element
                // fmt.Println(packet)
            }
        }()
    })

    fmt.Println("Writing to tracks")
    vp := NewVideoProcessor()
    go vp.writeH264ToTrackAR(videoTrack)
	// go vp.writeH264ToTrackFFmpegFilters(videoTrack)
    return nil
}

func establishSSHTunnel() (*exec.Cmd, error) {
	cmd := exec.Command("ssh", "-L", "5005:127.0.0.1:5005", "-J", "fastvideo", "-i", "~/.ssh/picluster", "epl@10.100.1.165")
	err := cmd.Start()
	if err != nil {
		return nil, fmt.Errorf("failed to start SSH tunnel: %w", err)
	}

	fmt.Println("SSH Tunnel established")
	return cmd, nil
}

func closeSSHTunnel(cmd *exec.Cmd) error {
	err := cmd.Wait()
	if err != nil {
		return fmt.Errorf("error waiting for SSH tunnel process: %w", err)
	}
	return nil
}

func (vp *VideoProcessor) writeH264ToTrackAR(track *webrtc.TrackLocalStaticSample) {
	defer vp.freeVideoCoding()

    conn, err := net.Dial("tcp", "127.0.0.1:5005")
    if err != nil {
        panic(err)
    }
    defer conn.Close()

    ticker := time.NewTicker(h264FrameDuration)
	for ; true; <-ticker.C {
		startTime := time.Now()
		
		if err = vp.inputFormatContext.ReadFrame(vp.decodePacket); err != nil {
			if errors.Is(err, astiav.ErrEof) {
				break
			}
			panic(err)
		}
		vp.decodePacket.RescaleTs(vp.videoStream.TimeBase(), vp.decodeCodecContext.TimeBase())

		if err = vp.decodeCodecContext.SendPacket(vp.decodePacket); err != nil {
			panic(err)
		}
	
		for {
			if err = vp.decodeCodecContext.ReceiveFrame(vp.decodeFrame); err != nil {
				if errors.Is(err, astiav.ErrEof) || errors.Is(err, astiav.ErrEagain) {
					// fmt.Println("Error while receiving decoded framed: ", err)
					break
				}
				panic(err)
			}

			if err = vp.convertToRGBAContext.ScaleFrame(vp.decodeFrame, vp.rgbaFrame); err != nil {
				panic(err)
			}

			vp.pts++
			vp.rgbaFrame.SetPts(vp.pts)

			startTime2 := time.Now()
            vp.arFilterFrame, err = OverlayARFilter(conn, vp.rgbaFrame)
			if err != nil {
				fmt.Println("Failed to add AR filter to frame: ", err)
			}
			elapsedTime2 := time.Since(startTime2)
			fmt.Printf("Time taken for adding AR filter: %v\n", elapsedTime2) 

			if err = vp.convertToYUV420PContext.ScaleFrame(vp.arFilterFrame, vp.yuv420PFrame); err != nil {
				panic(err)
			}

			vp.pts++
			vp.yuv420PFrame.SetPts(vp.pts)
			
			if err = vp.encodeCodecContext.SendFrame(vp.yuv420PFrame); err != nil {
				panic(err)
			}

			for {
				// Read encoded packets
				vp.encodePacket = astiav.AllocPacket()
				if err = vp.encodeCodecContext.ReceivePacket(vp.encodePacket); err != nil {
					if errors.Is(err, astiav.ErrEof) || errors.Is(err, astiav.ErrEagain) {
						break
					}
					panic(err)
				}

				// Write H264 to track
				if err = track.WriteSample(media.Sample{Data: vp.encodePacket.Data(), Duration: h264FrameDuration}); err != nil {
					panic(err)
				}
			}
		}
		elapsedTime := time.Since(startTime)
		fmt.Printf("Time taken from reading the packet, decoding, adding AR filter, encoding to writing in the WebRTC track: %v\n", elapsedTime) 
	}
}

func (vp *VideoProcessor) writeH264ToTrackFFmpegFilters(track *webrtc.TrackLocalStaticSample) {
	defer vp.freeVideoCoding()
	vp.initFilters()

	var err error

    ticker := time.NewTicker(h264FrameDuration)
	for ; true; <-ticker.C {
		startTime := time.Now()
		if err = vp.inputFormatContext.ReadFrame(vp.decodePacket); err != nil {
			if errors.Is(err, astiav.ErrEof) {
				break
			}
			panic(err)
		}
		vp.decodePacket.RescaleTs(vp.videoStream.TimeBase(), vp.decodeCodecContext.TimeBase())

		if err = vp.decodeCodecContext.SendPacket(vp.decodePacket); err != nil {
			panic(err)
		}
	
		for {
			if err = vp.decodeCodecContext.ReceiveFrame(vp.decodeFrame); err != nil {
				if errors.Is(err, astiav.ErrEof) || errors.Is(err, astiav.ErrEagain) {
					fmt.Println("Error while receiving decoded framed: ", err)
					break
				}
				panic(err)
			}
			
			if err = vp.buffersrcContext.AddFrame(vp.decodeFrame, astiav.NewBuffersrcFlags(astiav.BuffersrcFlagKeepRef)); err != nil {
				err = fmt.Errorf("main: adding frame failed: %w", err)
				return
			}

			for{
				vp.filterFrame.Unref()
				if err = vp.buffersinkContext.GetFrame(vp.filterFrame, astiav.NewBuffersinkFlags()); err != nil {
					if errors.Is(err, astiav.ErrEof) || errors.Is(err, astiav.ErrEagain) {
						break
					}
					panic(err)
				}
	
				vp.pts++
				vp.filterFrame.SetPts(vp.pts)

				if err = vp.encodeCodecContext.SendFrame(vp.filterFrame); err != nil {
					panic(err)
				}

				for {
					// Read encoded packets
					vp.encodePacket = astiav.AllocPacket()
					if err = vp.encodeCodecContext.ReceivePacket(vp.encodePacket); err != nil {
						if errors.Is(err, astiav.ErrEof) || errors.Is(err, astiav.ErrEagain) {
							break
						}
						panic(err)
					}
	
					// Write H264 to track
					if err = track.WriteSample(media.Sample{Data: vp.encodePacket.Data(), Duration: h264FrameDuration}); err != nil {
						panic(err)
					}
				}
			}
		}
		elapsedTime := time.Since(startTime)
		fmt.Printf("Time taken from reading the packet, decoding, adding AR filter, encoding to writing in the WebRTC track: %v\n", elapsedTime) 
	}
}
