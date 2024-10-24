package client

import (
	"fmt"
	"log"

	"github.com/gorilla/websocket"
	"github.com/pion/webrtc/v3"
)

type Message struct {
    Type    string `json:"type"`
    Content string `json:"content"`
}

var (
    answerChan = make(chan string) // Global variable for the channel
)


func handleOffer(conn *websocket.Conn, msg Message){
    fmt.Println("Received offer")
    peerConnection, videoTrack, err := createPeerConnection(conn)
    if err != nil {
		log.Fatal("Failed to create peer connection: ", err)
    }
    offerSDP := webrtc.SessionDescription{
        Type: webrtc.SDPTypeOffer,
        SDP: msg.Content,
    }
    
    if err := peerConnection.SetRemoteDescription(offerSDP); err != nil {
        log.Fatal("Failed to set remote description: ", err)
    }

	// Create answer 
    answer, err := peerConnection.CreateAnswer(nil)
    if err != nil {
        log.Fatal("Failed to create answer: ", err)
    }

    // Set the local description
    if err = peerConnection.SetLocalDescription(answer); err != nil {
        log.Fatal("Failed to set local description: ", err)
    }

    answerMsg := Message{
        Type:    "answer",
        Content: answer.SDP,
    }
    conn.WriteJSON(answerMsg)

    userPeerConnection = peerConnection
    userVideoTrack = videoTrack
    connectionEstablishedChan <- true
}

func handleAnswer(msg Message){
    answerChan <- msg.Content
}

func addICECandidate(msg Message){
    fmt.Println("Received ICE Candidate:", msg.Content)

    if (userPeerConnection == nil){
        fmt.Println("Peer connection not created yet. Returning...")
        return
    }

    // Create a new ICE candidate from the received content
    candidate := webrtc.ICECandidateInit{
        Candidate: msg.Content, 
    }

    // Add the ICE candidate to the peer connection
    if err := userPeerConnection.AddICECandidate(candidate); err != nil {
        fmt.Println("Failed to add ICE candidate:", err)
        return
    }

    fmt.Println("ICE Candidate added successfully.")
}
