package client

import (
	"errors"
	"fmt"
	"image/color"
	"log"
	"net"
	"os/exec"
	"time"

	"github.com/asticode/go-astiav"
	"github.com/pion/webrtc/v3"
	"github.com/pion/webrtc/v3/pkg/media"
	"gonum.org/v1/plot"
	"gonum.org/v1/plot/plotter"
	"gonum.org/v1/plot/vg"
)

var (
    timeChan = make(chan float64)
)


func openCameraFeed(peerConnection *webrtc.PeerConnection, videoTrack *webrtc.TrackLocalStaticSample, generate_stats bool) error {
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
	if(generate_stats){
		go generate_plots()
	}
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

func generate_plots() {
    p := plot.New()
    p.Title.Text = "Plot for processing time for each frame"
    p.X.Label.Text = "Frame"
    p.Y.Label.Text = "Time (ms)"
    p.Add(plotter.NewGrid())

    line, err := plotter.NewLine(plotter.XYs{})
    if err != nil {
        panic(err)
    }
    line.Color = color.RGBA{B: 255, A: 255}
    p.Add(line)

	frame_no := 0
    for data := range timeChan {
		fmt.Println("Received time: ", data)
        line.XYs = append(line.XYs, plotter.XY{X: float64(frame_no), Y: data})
        frame_no += 1
        
        if len(line.XYs) > 200 {
            line.XYs = line.XYs[1:]
        }

        xMin := float64(frame_no) - 200.0
        if xMin < 0 {
            xMin = 0
        }
        p.X.Min = xMin
        p.X.Max = float64(frame_no)

        yMin := data
        yMax := data
        for _, point := range line.XYs {
            if point.Y < yMin {
                yMin = point.Y
            }
            if point.Y > yMax {
                yMax = point.Y
            }
        }
        padding := (yMax - yMin) * 0.1
        p.Y.Min = yMin - padding
        p.Y.Max = yMax + padding

        filename := fmt.Sprintf("plots/plot_%d.png", frame_no)
        if err := p.Save(6*vg.Inch, 4*vg.Inch, filename); err != nil {
            panic(err)
        }
    }
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
			_ = time.Since(startTime2)
			// timeChan <- float64(elapsedTime2.Milliseconds())

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
		timeChan <- float64(elapsedTime.Milliseconds())
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
					// fmt.Println("Error while receiving decoded framed: ", err)
					break
				}
				panic(err)
			}
			
			// startTime2 := time.Now()
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
			// elapsedTime2 := time.Since(startTime2)
			// timeChan <- float64(elapsedTime2.Milliseconds())
		}
		elapsedTime := time.Since(startTime)
		timeChan <- float64(elapsedTime.Milliseconds())
	}
}
