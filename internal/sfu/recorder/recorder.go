// Package recorder provides server-side audio recording with processing
// This integrates the audio processor for noise reduction and quality enhancement
package recorder

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"

	"Remainwith/internal/sfu/audio"

	"github.com/pion/opus"
	"github.com/pion/rtp"
)

// RecorderConfig configures the audio recorder
type RecorderConfig struct {
	// Output directory for recordings
	OutputDir string

	// Enable audio processing
	EnableProcessing bool

	// Audio processing config (used if EnableProcessing is true)
	AudioConfig audio.ProcessorConfig

	// File format
	Format string // "wav" or "raw"

	// Maximum recording duration
	MaxDuration time.Duration
}

// DefaultRecorderConfig returns default configuration
func DefaultRecorderConfig() RecorderConfig {
	return RecorderConfig{
		OutputDir:        "./recordings",
		EnableProcessing: true,
		AudioConfig:      audio.DefaultConfig(),
		Format:           "wav",
		MaxDuration:      2 * time.Hour,
	}
}

// AudioRecorder records and processes audio streams
type AudioRecorder struct {
	config RecorderConfig
	mu     sync.RWMutex

	// Active recordings
	recordings map[string]*RecordingSession

	// Opus decoder
	decoder opus.Decoder

	// Audio processor
	processor *audio.Processor
}

// RecordingSession represents an active recording
type RecordingSession struct {
	ID        string
	RoomID    string
	ClientID  string
	TrackID   string
	StartTime time.Time

	// File handling
	file     *os.File
	filename string

	// Audio processing
	processor   *audio.Processor
	pcmBuffer   []int16
	opusBuffer  []byte
	sampleCount int

	// Control
	done    chan struct{}
	stopped bool
}

// NewAudioRecorder creates a new audio recorder
func NewAudioRecorder(config RecorderConfig) (*AudioRecorder, error) {
	// Create output directory
	if err := os.MkdirAll(config.OutputDir, 0755); err != nil {
		return nil, fmt.Errorf("create output directory: %w", err)
	}

	// Create Opus decoder
	decoder := opus.NewDecoder()

	// Create audio processor if enabled
	var processor *audio.Processor
	if config.EnableProcessing {
		// Disable AEC for recording (no speaker reference)
		config.AudioConfig.EchoCancellation = false
		proc, err := audio.NewProcessor(config.AudioConfig)
		if err != nil {
			log.Printf("Recorder: Failed to create audio processor, continuing without: %v", err)
		} else {
			processor = proc
			log.Printf("Recorder: Audio processing enabled (NS=%v AGC=%v HPF=%v)",
				config.AudioConfig.NoiseSuppression,
				config.AudioConfig.AutomaticGainControl,
				config.AudioConfig.HighPassFilter)
		}
	}

	return &AudioRecorder{
		config:     config,
		recordings: make(map[string]*RecordingSession),
		decoder:    decoder,
		processor:  processor,
	}, nil
}

// StartRecording starts recording an audio track
func (r *AudioRecorder) StartRecording(roomID, clientID, trackID string) (*RecordingSession, error) {
	sessionID := fmt.Sprintf("%s_%s_%s_%d", roomID, clientID, trackID, time.Now().Unix())

	r.mu.Lock()
	defer r.mu.Unlock()

	// Check if already recording
	if _, exists := r.recordings[sessionID]; exists {
		return nil, fmt.Errorf("recording already exists: %s", sessionID)
	}

	// Create filename
	timestamp := time.Now().Format("20060102_150405")
	filename := fmt.Sprintf("%s_%s_%s_%s.wav", roomID, clientID, trackID, timestamp)
	filepath := filepath.Join(r.config.OutputDir, filename)

	// Create file
	file, err := os.Create(filepath)
	if err != nil {
		return nil, fmt.Errorf("create recording file: %w", err)
	}

	// Write WAV header (will be updated when recording stops)
	if err := writeWAVHeader(file, 48000, 1, 16); err != nil {
		file.Close()
		return nil, fmt.Errorf("write wav header: %w", err)
	}

	// Create audio processor for this session
	var sessionProcessor *audio.Processor
	if r.config.EnableProcessing && r.processor != nil {
		// Clone the processor config for this session
		config := r.config.AudioConfig
		config.EchoCancellation = false // No AEC for recording
		proc, err := audio.NewProcessor(config)
		if err != nil {
			log.Printf("Recorder: Failed to create session processor: %v", err)
		} else {
			sessionProcessor = proc
		}
	}

	session := &RecordingSession{
		ID:         sessionID,
		RoomID:     roomID,
		ClientID:   clientID,
		TrackID:    trackID,
		StartTime:  time.Now(),
		file:       file,
		filename:   filepath,
		processor:  sessionProcessor,
		pcmBuffer:  make([]int16, 480*10), // 10ms * 10 = 100ms buffer
		opusBuffer: make([]byte, 1500),    // Max Opus packet size
		done:       make(chan struct{}),
	}

	r.recordings[sessionID] = session

	log.Printf("Recorder: Started recording %s -> %s", sessionID, filename)

	// Start max duration timer
	if r.config.MaxDuration > 0 {
		go func() {
			select {
			case <-time.After(r.config.MaxDuration):
				log.Printf("Recorder: Max duration reached for %s", sessionID)
				r.StopRecording(sessionID)
			case <-session.done:
				return
			}
		}()
	}

	return session, nil
}

// WriteRTP writes an RTP packet to the recording
func (r *AudioRecorder) WriteRTP(sessionID string, packet *rtp.Packet) error {
	r.mu.RLock()
	session, exists := r.recordings[sessionID]
	r.mu.RUnlock()

	if !exists {
		return fmt.Errorf("recording not found: %s", sessionID)
	}

	if session.stopped {
		return fmt.Errorf("recording already stopped: %s", sessionID)
	}

	// Decode Opus to PCM (output as bytes)
	pcmBytes := make([]byte, len(session.pcmBuffer)*2) // int16 to bytes
	bandwidth, isStereo, err := r.decoder.Decode(packet.Payload, pcmBytes)
	if err != nil {
		// Log but don't fail - Opus packets can be malformed
		log.Printf("Recorder: Opus decode error for %s: %v", sessionID, err)
		return nil // Continue recording
	}

	// Convert bytes to int16 samples
	pcmSamples := bytesToInt16(pcmBytes)

	// Log audio format on first packet
	if session.sampleCount == 0 {
		log.Printf("Recorder: Audio format - bandwidth: %v, stereo: %v", bandwidth, isStereo)
	}

	// Apply audio processing if enabled
	if session.processor != nil {
		// Convert to float32
		floatSamples := make([]float32, len(pcmSamples))
		for i, s := range pcmSamples {
			floatSamples[i] = float32(s) / 32768.0
		}

		// Process
		processed, err := session.processor.ProcessFrame(floatSamples)
		if err != nil {
			log.Printf("Recorder: Audio processing error: %v", err)
		} else {
			// Convert back to int16
			for i, f := range processed {
				// Clamp
				if f > 1.0 {
					f = 1.0
				} else if f < -1.0 {
					f = -1.0
				}
				pcmSamples[i] = int16(f * 32767.0)
			}
		}
	}

	// Write PCM to file
	if err := writePCM(session.file, pcmSamples); err != nil {
		return fmt.Errorf("write pcm: %w", err)
	}

	session.sampleCount += len(pcmSamples)

	return nil
}

// StopRecording stops a recording and finalizes the file
func (r *AudioRecorder) StopRecording(sessionID string) error {
	r.mu.Lock()
	session, exists := r.recordings[sessionID]
	if !exists {
		r.mu.Unlock()
		return fmt.Errorf("recording not found: %s", sessionID)
	}

	// Mark as stopped
	session.stopped = true
	close(session.done)
	delete(r.recordings, sessionID)
	r.mu.Unlock()

	// Close processor
	if session.processor != nil {
		session.processor.Close()
	}

	// Update WAV header with final size
	if err := updateWAVHeader(session.file, session.sampleCount); err != nil {
		log.Printf("Recorder: Error updating WAV header: %v", err)
	}

	// Close file
	if err := session.file.Close(); err != nil {
		return fmt.Errorf("close file: %w", err)
	}

	duration := time.Since(session.StartTime)
	log.Printf("Recorder: Stopped recording %s (duration: %v, samples: %d)",
		sessionID, duration, session.sampleCount)

	return nil
}

// GetActiveRecordings returns list of active recordings
func (r *AudioRecorder) GetActiveRecordings() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	ids := make([]string, 0, len(r.recordings))
	for id := range r.recordings {
		ids = append(ids, id)
	}
	return ids
}

// Close stops all recordings and cleans up
func (r *AudioRecorder) Close() error {
	r.mu.Lock()
	sessions := make([]*RecordingSession, 0, len(r.recordings))
	for _, session := range r.recordings {
		sessions = append(sessions, session)
	}
	r.recordings = make(map[string]*RecordingSession)
	r.mu.Unlock()

	// Stop all recordings
	for _, session := range sessions {
		if err := r.StopRecording(session.ID); err != nil {
			log.Printf("Recorder: Error stopping recording %s: %v", session.ID, err)
		}
	}

	// Close main processor
	if r.processor != nil {
		r.processor.Close()
	}

	return nil
}

// writeWAVHeader writes initial WAV file header
func writeWAVHeader(file *os.File, sampleRate, channels, bitsPerSample int) error {
	// WAV header structure
	header := make([]byte, 44)

	// RIFF chunk
	copy(header[0:4], "RIFF")
	// File size (will be updated later)
	header[4] = 0xFF
	header[5] = 0xFF
	header[6] = 0xFF
	header[7] = 0xFF

	// WAVE format
	copy(header[8:12], "WAVE")

	// fmt chunk
	copy(header[12:16], "fmt ")
	header[16] = 16 // Subchunk size
	header[17] = 0
	header[18] = 0
	header[19] = 0
	header[20] = 1 // Audio format (PCM)
	header[21] = 0
	header[22] = byte(channels)
	header[23] = 0
	header[24] = byte(sampleRate)
	header[25] = byte(sampleRate >> 8)
	header[26] = byte(sampleRate >> 16)
	header[27] = byte(sampleRate >> 24)
	byteRate := sampleRate * channels * bitsPerSample / 8
	header[28] = byte(byteRate)
	header[29] = byte(byteRate >> 8)
	header[30] = byte(byteRate >> 16)
	header[31] = byte(byteRate >> 24)
	header[32] = byte(channels * bitsPerSample / 8) // Block align
	header[33] = 0
	header[34] = byte(bitsPerSample)
	header[35] = 0

	// data chunk
	copy(header[36:40], "data")
	// Data size (will be updated later)
	header[40] = 0xFF
	header[41] = 0xFF
	header[42] = 0xFF
	header[43] = 0xFF

	_, err := file.Write(header)
	return err
}

// updateWAVHeader updates the WAV header with final sizes
func updateWAVHeader(file *os.File, sampleCount int) error {
	// Calculate sizes
	dataSize := sampleCount * 2 // 16-bit samples
	fileSize := dataSize + 36   // + header size

	// Update RIFF chunk size at offset 4
	if _, err := file.Seek(4, 0); err != nil {
		return err
	}
	sizeBytes := []byte{
		byte(fileSize),
		byte(fileSize >> 8),
		byte(fileSize >> 16),
		byte(fileSize >> 24),
	}
	if _, err := file.Write(sizeBytes); err != nil {
		return err
	}

	// Update data chunk size at offset 40
	if _, err := file.Seek(40, 0); err != nil {
		return err
	}
	dataBytes := []byte{
		byte(dataSize),
		byte(dataSize >> 8),
		byte(dataSize >> 16),
		byte(dataSize >> 24),
	}
	_, err := file.Write(dataBytes)
	return err
}

// writePCM writes PCM samples to file
func writePCM(file *os.File, samples []int16) error {
	// Convert to bytes
	data := int16ToBytes(samples)
	_, err := file.Write(data)
	return err
}

// bytesToInt16 converts byte buffer to int16 samples (little-endian)
func bytesToInt16(data []byte) []int16 {
	if len(data)%2 != 0 {
		data = append(data, 0)
	}
	samples := make([]int16, len(data)/2)
	for i := 0; i < len(data); i += 2 {
		samples[i/2] = int16(data[i]) | int16(data[i+1])<<8
	}
	return samples
}

// int16ToBytes converts int16 samples to byte buffer (little-endian)
func int16ToBytes(samples []int16) []byte {
	data := make([]byte, len(samples)*2)
	for i, sample := range samples {
		data[i*2] = byte(sample)
		data[i*2+1] = byte(sample >> 8)
	}
	return data
}
