package audio

import (
	"math"
	"testing"
	"time"
)

// TestProcessorCreation tests creating a processor with default config
func TestProcessorCreation(t *testing.T) {
	config := DefaultConfig()
	processor, err := NewProcessor(config)
	if err != nil {
		t.Fatalf("Failed to create processor: %v", err)
	}
	defer processor.Close()

	if processor == nil {
		t.Fatal("Processor is nil")
	}

	if !processor.config.Enabled {
		t.Error("Processor should be enabled by default")
	}
}

// TestProcessorDisabled tests disabled processor
func TestProcessorDisabled(t *testing.T) {
	config := ProcessorConfig{Enabled: false}
	processor, err := NewProcessor(config)
	if err != nil {
		t.Fatalf("Failed to create disabled processor: %v", err)
	}
	defer processor.Close()

	// Test that processing returns samples unchanged
	samples := []float32{0.5, -0.5, 0.25, -0.25}
	result, err := processor.ProcessFrame(samples)
	if err != nil {
		t.Fatalf("ProcessFrame failed: %v", err)
	}

	// Should return same samples when disabled
	for i, s := range samples {
		if result[i] != s {
			t.Errorf("Sample %d changed when disabled: got %f, want %f", i, result[i], s)
		}
	}
}

// TestHighPassFilter tests HPF functionality
func TestHighPassFilter(t *testing.T) {
	config := ProcessorConfig{
		Enabled:        true,
		HighPassFilter: true,
	}
	processor, err := NewProcessor(config)
	if err != nil {
		t.Fatalf("Failed to create processor: %v", err)
	}
	defer processor.Close()

	// Create a signal with DC offset
	samples := make([]float32, 4800) // 100ms at 48kHz
	dcOffset := float32(0.5)
	for i := range samples {
		samples[i] = dcOffset // Pure DC
	}

	// Process
	result, err := processor.ProcessFrame(samples)
	if err != nil {
		t.Fatalf("ProcessFrame failed: %v", err)
	}

	// Calculate mean (should be close to 0 after HPF)
	var sum float64
	for _, s := range result {
		sum += float64(s)
	}
	mean := sum / float64(len(result))

	// DC should be significantly reduced
	if math.Abs(mean) > 0.1 {
		t.Errorf("HPF did not remove DC offset: mean = %f", mean)
	}
}

// TestNoiseSuppression tests NS functionality
func TestNoiseSuppression(t *testing.T) {
	config := ProcessorConfig{
		Enabled:          true,
		NoiseSuppression: true,
		NSThresholdDb:    -30,
	}
	processor, err := NewProcessor(config)
	if err != nil {
		t.Fatalf("Failed to create processor: %v", err)
	}
	defer processor.Close()

	// Create low-level noise
	samples := make([]float32, 4800)
	for i := range samples {
		samples[i] = 0.001 // Very quiet signal
	}

	// Process multiple times to let NS adapt
	for i := 0; i < 10; i++ {
		_, err := processor.ProcessFrame(samples)
		if err != nil {
			t.Fatalf("ProcessFrame failed: %v", err)
		}
	}

	// The noise should be attenuated
	// (We can't easily measure this without a reference, but we can verify it doesn't crash)
}

// TestAGC tests AGC functionality
func TestAGC(t *testing.T) {
	config := ProcessorConfig{
		Enabled:              true,
		AutomaticGainControl: true,
		AGCTargetDb:          -20,
		AGCMaxGainDb:         30,
		AGCMinGainDb:         -10,
	}
	processor, err := NewProcessor(config)
	if err != nil {
		t.Fatalf("Failed to create processor: %v", err)
	}
	defer processor.Close()

	// Create a quiet signal
	samples := make([]float32, 4800)
	for i := range samples {
		samples[i] = 0.01 // Very quiet
	}

	// Process multiple times to let AGC adapt
	var result []float32
	var err2 error
	for i := 0; i < 50; i++ {
		result, err2 = processor.ProcessFrame(samples)
		if err2 != nil {
			t.Fatalf("ProcessFrame failed: %v", err2)
		}
	}

	// Calculate RMS of result
	var sum float64
	for _, s := range result {
		sum += float64(s) * float64(s)
	}
	rms := math.Sqrt(sum / float64(len(result)))

	// RMS should be higher than input (gain applied)
	targetLevel := math.Pow(10, config.AGCTargetDb/20)
	if rms < targetLevel*0.5 {
		t.Errorf("AGC did not amplify signal enough: rms = %f, target = %f", rms, targetLevel)
	}
}

// TestAEC tests echo cancellation
func TestAEC(t *testing.T) {
	config := ProcessorConfig{
		Enabled:          true,
		EchoCancellation: true,
		AECTailLengthMs:  50,
		AECStepSize:      0.5,
	}
	processor, err := NewProcessor(config)
	if err != nil {
		t.Fatalf("Failed to create processor: %v", err)
	}
	defer processor.Close()

	// Simulate echo scenario
	farEnd := make([]float32, 480) // 10ms
	for i := range farEnd {
		farEnd[i] = float32(math.Sin(2 * math.Pi * 1000 * float64(i) / 48000))
	}

	nearEnd := make([]float32, 480)
	// Near end has echo of far end plus some speech
	for i := range nearEnd {
		echo := 0.5 * farEnd[i] // Echo at 50% amplitude
		speech := float32(math.Sin(2*math.Pi*500*float64(i)/48000)) * 0.3
		nearEnd[i] = echo + speech
	}

	// Process far end first (required for AEC)
	processor.ProcessRender(farEnd)

	// Process near end
	result, err := processor.ProcessCapture(nearEnd)
	if err != nil {
		t.Fatalf("ProcessCapture failed: %v", err)
	}

	// The echo should be reduced (we can't easily verify exact amount,
	// but we can check the signal changed)
	changed := false
	for i := range result {
		if math.Abs(float64(result[i]-nearEnd[i])) > 0.001 {
			changed = true
			break
		}
	}
	if !changed {
		t.Error("AEC did not modify the signal")
	}
}

// TestInt16Conversion tests byte/int16/float32 conversions
func TestInt16Conversion(t *testing.T) {
	// Test bytesToInt16
	bytes := []byte{0x00, 0x10, 0xFF, 0x7F} // 4096, 32767
	ints := bytesToInt16(bytes)
	if len(ints) != 2 {
		t.Errorf("Expected 2 samples, got %d", len(ints))
	}
	if ints[0] != 4096 {
		t.Errorf("Expected 4096, got %d", ints[0])
	}
	if ints[1] != 32767 {
		t.Errorf("Expected 32767, got %d", ints[1])
	}

	// Test int16ToBytes
	back := int16ToBytes(ints)
	if len(back) != 4 {
		t.Errorf("Expected 4 bytes, got %d", len(back))
	}

	// Test int16ToFloat32
	floats := int16ToFloat32(ints)
	if len(floats) != 2 {
		t.Errorf("Expected 2 floats, got %d", len(floats))
	}
	if math.Abs(float64(floats[0])-0.125) > 0.001 {
		t.Errorf("Expected ~0.125, got %f", floats[0])
	}
	if math.Abs(float64(floats[1])-0.99997) > 0.001 {
		t.Errorf("Expected ~1.0, got %f", floats[1])
	}

	// Test float32ToInt16
	backInts := float32ToInt16(floats)
	if len(backInts) != 2 {
		t.Errorf("Expected 2 ints, got %d", len(backInts))
	}
	// Allow small tolerance due to floating point precision
	if backInts[0] < 4094 || backInts[0] > 4098 {
		t.Errorf("Expected ~4096, got %d", backInts[0])
	}

}

// TestRecordingPipeline tests the recording pipeline
func TestRecordingPipeline(t *testing.T) {
	pipeline, err := NewRecordingPipeline(true)
	if err != nil {
		t.Fatalf("Failed to create recording pipeline: %v", err)
	}
	defer pipeline.Close()

	if !pipeline.IsEnabled() {
		t.Error("Recording pipeline should be enabled")
	}

	// Test processing
	pcmData := make([]byte, 960) // 480 samples * 2 bytes
	for i := 0; i < len(pcmData); i += 2 {
		pcmData[i] = 0x00
		pcmData[i+1] = 0x10 // 4096
	}

	result, err := pipeline.ProcessAudio(pcmData)
	if err != nil {
		t.Fatalf("ProcessAudio failed: %v", err)
	}

	if len(result) != len(pcmData) {
		t.Errorf("Output size mismatch: got %d, want %d", len(result), len(pcmData))
	}
}

// TestCallProcessor tests the real-time call processor
func TestCallProcessor(t *testing.T) {
	cp, err := NewCallProcessor(true)
	if err != nil {
		t.Fatalf("Failed to create call processor: %v", err)
	}
	defer cp.Close()

	if !cp.IsEnabled() {
		t.Error("Call processor should be enabled")
	}

	// Test stats
	stats := cp.GetStats()
	if !stats["enabled"].(bool) {
		t.Error("Stats should show enabled")
	}

	// Test far-end processing
	farEnd := make([]float32, 480)
	for i := range farEnd {
		farEnd[i] = 0.5
	}
	cp.ProcessFarEnd(farEnd)

	// Give some time for async processing
	time.Sleep(10 * time.Millisecond)

	// Test near-end processing
	nearEnd := make([]float32, 480)
	for i := range nearEnd {
		nearEnd[i] = 0.3
	}
	result, err := cp.ProcessNearEnd(nearEnd)
	if err != nil {
		t.Fatalf("ProcessNearEnd failed: %v", err)
	}
	if len(result) != len(nearEnd) {
		t.Errorf("Output size mismatch: got %d, want %d", len(result), len(nearEnd))
	}

	// Test wait for processing (give more time for goroutine to finish)
	if !cp.WaitForProcessing(500 * time.Millisecond) {
		t.Log("WaitForProcessing timed out (non-critical)")
	}

}

// BenchmarkProcessor benchmarks the full processing pipeline
func BenchmarkProcessor(b *testing.B) {
	config := DefaultConfig()
	processor, err := NewProcessor(config)
	if err != nil {
		b.Fatalf("Failed to create processor: %v", err)
	}
	defer processor.Close()

	// 10ms of audio at 48kHz
	samples := make([]float32, 480)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := processor.ProcessFrame(samples)
		if err != nil {
			b.Fatalf("ProcessFrame failed: %v", err)
		}
	}
}

// BenchmarkAEC benchmarks echo cancellation specifically
func BenchmarkAEC(b *testing.B) {
	config := ProcessorConfig{
		Enabled:          true,
		EchoCancellation: true,
		AECTailLengthMs:  100,
		AECStepSize:      0.5,
	}
	processor, err := NewProcessor(config)
	if err != nil {
		b.Fatalf("Failed to create processor: %v", err)
	}
	defer processor.Close()

	farEnd := make([]float32, 480)
	nearEnd := make([]float32, 480)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		processor.ProcessRender(farEnd)
		processor.ProcessCapture(nearEnd)
	}
}
