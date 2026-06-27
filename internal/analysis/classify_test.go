package analysis_test

import (
	"testing"

	"pp-starlink/internal/analysis"
)

func fptr(f float64) *float64 { return &f }

func TestDiagnoseCause(t *testing.T) {
	tests := []struct {
		name string
		p    analysis.DiagnoseParams
		want string
	}{
		{
			name: "SNR dip triggers RF blockage",
			p:    analysis.DiagnoseParams{BeaconSNR: fptr(5), BaselineSNR: fptr(10), SNRDelta: 3, NoiseDelta: 3, IsSnrAboveNoiseFloor: true},
			want: analysis.CauseRFBlockage,
		},
		{
			name: "noise spike triggers EMI",
			p:    analysis.DiagnoseParams{NoiseFloor: fptr(15), BaselineNoise: fptr(10), SNRDelta: 3, NoiseDelta: 3, IsSnrAboveNoiseFloor: true},
			want: analysis.CauseRFInterfer,
		},
		{
			name: "lower signal flag triggers dish signal",
			p:    analysis.DiagnoseParams{LowerSignalThanPredicted: true, SNRDelta: 3, NoiseDelta: 3, IsSnrAboveNoiseFloor: true},
			want: analysis.CauseDishSignal,
		},
		{
			name: "SNR not above noise floor triggers dish signal",
			p:    analysis.DiagnoseParams{IsSnrAboveNoiseFloor: false, SNRDelta: 3, NoiseDelta: 3},
			want: analysis.CauseDishSignal,
		},
		{
			name: "no signal data defaults to congestion",
			p:    analysis.DiagnoseParams{IsSnrAboveNoiseFloor: true, SNRDelta: 3, NoiseDelta: 3},
			want: analysis.CauseCongestion,
		},
		{
			name: "RF blockage wins over EMI when both fire",
			p: analysis.DiagnoseParams{
				BeaconSNR: fptr(4), BaselineSNR: fptr(10),
				NoiseFloor: fptr(15), BaselineNoise: fptr(10),
				SNRDelta: 3, NoiseDelta: 3, IsSnrAboveNoiseFloor: true,
			},
			want: analysis.CauseRFBlockage,
		},
		{
			name: "SNR exactly at threshold does not trigger",
			p:    analysis.DiagnoseParams{BeaconSNR: fptr(7), BaselineSNR: fptr(10), SNRDelta: 3, NoiseDelta: 3, IsSnrAboveNoiseFloor: true},
			want: analysis.CauseCongestion,
		},
		{
			name: "nil SNR pointers skip RF check",
			p:    analysis.DiagnoseParams{SNRDelta: 3, NoiseDelta: 3, IsSnrAboveNoiseFloor: true},
			want: analysis.CauseCongestion,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := analysis.DiagnoseCause(tc.p)
			if got != tc.want {
				t.Errorf("DiagnoseCause() = %q\n\t\t\t\twant %q", got, tc.want)
			}
		})
	}
}
