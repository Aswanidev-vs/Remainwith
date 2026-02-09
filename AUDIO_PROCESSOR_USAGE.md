# Audio Processor Usage Guide

The `internal/sfu/audio/processor.go` provides comprehensive server-side audio processing for WebRTC calls.

## Features Implemented

### 1. **AEC (Adaptive Echo Cancellation)**
- **Algorithm**: NLMS (Normalized Least Mean Squares)
- **Purpose**: Removes speaker audio from microphone input
- **How it works**: 
  - `ProcessRender()` - Feed far-end audio (what the speaker is playing)
  - `ProcessCapture()` - Process near-end audio (microphone input)
  - The AEC subtracts the estimated echo from the microphone signal

### 2. **NS (Noise Suppression)**
- **Algorithm**: Spectral noise gating with envelope follower
- **Purpose**: Reduces background noise (keyboard typing, fans, etc.)
- **How it works**: 
  - Estimates noise floor dynamically
  - Applies gain reduction when signal is below threshold
  - Smooth attack/release prevents audio artifacts

### 3. **AGC (Automatic Gain Control)**
- **Algorithm**: RMS-based level normalization
- **Purpose**: Normalizes audio levels across different participants
- **How it works**:
  - Measures RMS level over 50ms window
  - Adjusts gain to reach target level (-20dB default)
  - Limits gain between -10dB and +30dB to prevent distortion

### 4. **HPF (High-Pass Filter)**
- **Algorithm**: First-order IIR filter
- **Purpose**: Removes low-frequency noise (AC hum, rumble)
- **Cutoff**: 80Hz default

## Usage Examples

### Basic Usage - Recording Pipeline

```go
package main

import (
    "Remainwith/internal/sfu/audio"
    "log"
)

func main() {
    // Create recording pipeline with audio processing
    // (AEC disabled for recording since there's no speaker reference)
    pipeline, err := audio.NewRecordingPipeline(true)
    if err != nil {
        log.Fatal(err)
    }
    defer pipeline.Close()

    // Process recorded audio
    pcmData := []byte{...} // 16-bit PCM audio
    processed, err := pipeline.ProcessAudio(pcmData)
    if err != nil {
        log.Fatal(err)
    }
    
    // processed now has noise suppression and AGC applied
}
```

### Real-Time Call Processing

```go
package main

import (
    "Remainwith/internal/sfu/audio"
    "log"
)

func main() {
    // Create call processor with all features enabled
    processor, err := audio.NewCallProcessor(true)
    if err != nil {
        log.Fatal(err)
    }
    defer processor.Close()

    // In your WebRTC handler:
    
    // 1. When receiving audio from remote peer (far-end)
    // This is what the local speaker is playing
    farEndSamples := []float32{...} // 48kHz float32 audio
    processor.ProcessFarEnd(farEndSamples)

    // 2. When capturing from local microphone (near-end)
    nearEndSamples := []float32{...} // 48kHz float32 audio
    processed, err := processor.ProcessNearEnd(nearEndSamples)
    if err != nil {
        log.Fatal(err)
    }
    
    // processed now has echo cancellation, noise suppression, and AGC
}
```

### Custom Configuration

```go
package main

import (
    "Remainwith/internal/sfu/audio"
)

func main() {
    // Create custom configuration
    config := audio.ProcessorConfig{
        Enabled:              true,
        EchoCancellation:     true,
        NoiseSuppression:     true,
        AutomaticGainControl: true,
        HighPassFilter:       true,

        // AEC settings
        AECTailLengthMs: 100,  // Echo tail length
        AECStepSize:     0.5,   // NLMS adaptation rate

        // NS settings
        NSThresholdDb: -40,    // Noise gate threshold
        NSAttackMs:    10,      // Attack time
        NSReleaseMs:   100,     // Release time

        // AGC settings
        AGCTargetDb:  -20,      // Target level
        AGCMaxGainDb: 30,       // Max gain
        AGCMinGainDb: -10,      // Min gain
        AGCAttackMs:  50,       // Attack time
        AGCReleaseMs: 500,      // Release time

        // HPF settings
        HPCutoffHz: 80,         // High-pass cutoff
    }

    processor, err := audio.NewProcessor(config)
    if err != nil {
        panic(err)
    }
    defer processor.Close()

    // Process audio frame (10ms at 48kHz = 480 samples)
    samples := make([]float32, 480)
    processed, err := processor.ProcessFrame(samples)
    if err != nil {
        panic(err)
    }
}
```

## Integration with SFU

To integrate the audio processor with the SFU for real-time call processing:

```go
// In internal/sfu/sfu.go, modify the track handling:

import "Remainwith/internal/sfu/audio"

type Client struct {
    // ... existing fields ...
    audioProcessor *audio.CallProcessor
}

func (s *SFU) ServeHTTP(w http.ResponseWriter, r *http.Request) {
    // ... existing code ...
    
    // Create audio processor for this client
    audioProcessor, err := audio.NewCallProcessor(true)
    if err != nil {
        log.Printf("SFU: Failed to create audio processor: %v", err)
        // Continue without audio processing
    }
    
    client := &Client{
        // ... existing fields ...
        audioProcessor: audioProcessor,
    }
    
    // ... rest of the code ...
}

// Process incoming audio track
func (s *SFU) processAudioTrack(client *Client, track *webrtc.TrackRemote) {
    if client.audioProcessor == nil {
        // No audio processing, forward as-is
        return
    }

    // Read RTP packets
    for {
        packet, _, err := track.ReadRTP()
        if err != nil {
            return
        }

        // Decode Opus to PCM
        pcmData := decodeOpus(packet.Payload)
        
        // Convert to float32
        floatSamples := int16ToFloat32(pcmData)
        
        // Process audio (AEC, NS, AGC, HPF)
        processed, err := client.audioProcessor.ProcessNearEnd(floatSamples)
        if err != nil {
            log.Printf("Audio processing error: %v", err)
            continue
        }
        
        // Convert back to int16
        outputData := float32ToInt16(processed)
        
        // Re-encode to Opus and forward
        // ... forwarding logic ...
    }
}
```

## Performance Benchmarks

From the test results on AMD Ryzen 5 5600H:

```
BenchmarkProcessor-12     100    10080049 ns/op  (~10ms per frame)
BenchmarkAEC-12           122     9568773 ns/op  (~9.6ms per frame)
```

**Performance Notes:**
- Processing 10ms of audio (480 samples at 48kHz) takes ~10ms
- This is real-time capable for single streams
- For multiple streams, consider:
  - Disabling AEC (most CPU intensive)
  - Using lower quality settings
  - Running on dedicated audio processing threads

## Configuration Recommendations

### For High-Quality Calls (Low Latency)
```go
config := audio.ProcessorConfig{
    Enabled:              true,
    EchoCancellation:     true,
    NoiseSuppression:     true,
    AutomaticGainControl: true,
    HighPassFilter:       true,
    
    AECTailLengthMs: 50,   // Shorter tail = lower latency
    AECStepSize:     0.3,  // Conservative adaptation
    NSThresholdDb:   -35,  // Less aggressive NS
    AGCAttackMs:     20,   // Fast attack
    AGCReleaseMs:    200,  // Fast release
}
```

### For Recording (Quality over Latency)
```go
config := audio.ProcessorConfig{
    Enabled:              true,
    EchoCancellation:     false, // No speaker reference
    NoiseSuppression:     true,
    AutomaticGainControl: true,
    HighPassFilter:       true,
    
    NSThresholdDb:   -40,  // More aggressive NS
    NSAttackMs:      10,
    NSReleaseMs:     100,
    AGCTargetDb:     -20,
    AGCMaxGainDb:    30,
    AGCMinGainDb:    -10,
}
```

### For Low-CPU Environments
```go
config := audio.ProcessorConfig{
    Enabled:              true,
    EchoCancellation:     false, // Disable AEC (CPU intensive)
    NoiseSuppression:     true,  // Keep NS (lightweight)
    AutomaticGainControl: true,  // Keep AGC (lightweight)
    HighPassFilter:       true,  // Keep HPF (very lightweight)
}
```

## Testing

Run the audio processor tests:
```bash
go test ./internal/sfu/audio/... -v
```

Run benchmarks:
```bash
go test ./internal/sfu/audio/... -bench=. -benchtime=1s
```

## Notes

1. **Sample Rate**: Fixed at 48kHz (Opus native rate)
2. **Format**: Input/output is float32 in range [-1.0, 1.0]
3. **Frame Size**: Recommended 10ms (480 samples) for real-time
4. **Thread Safety**: Processor is thread-safe with mutex protection
5. **AEC Requirements**: Must call `ProcessRender()` before `ProcessCapture()` with the same timestamp
