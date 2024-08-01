package parser

import (
	"testing"

	"github.com/dharmab/skyeye/pkg/bearings"
	"github.com/dharmab/skyeye/pkg/brevity"
	"github.com/martinlindhe/unit"
	"github.com/stretchr/testify/require"
)

func TestParserSnaplock(t *testing.T) {
	testCases := []parserTestCase{
		{
			text: "ANYFACE, FREEDOM 31, SNAPLOCK 125 10, 8000",
			expectedRequest: &brevity.SnaplockRequest{
				Callsign: "freedom 3 1",
				BRA: brevity.NewBRA(
					bearings.NewMagneticBearing(125*unit.Degree),
					10*unit.NauticalMile,
					8000*unit.Foot,
				),
			},
			expectedOk: true,
		},
		{
			text: "Anyface Fox 1 2 snap lock 0-5-8-147-3000",
			expectedRequest: &brevity.SnaplockRequest{
				Callsign: "fox 1 2",
				BRA: brevity.NewBRA(
					bearings.NewMagneticBearing(58*unit.Degree),
					147*unit.NauticalMile,
					3000*unit.Foot,
				),
			},
			expectedOk: true,
		},
	}
	runParserTestCases(t, New(TestCallsign), testCases, func(t *testing.T, test parserTestCase, request any) {
		expected := test.expectedRequest.(*brevity.SnaplockRequest)
		actual := request.(*brevity.SnaplockRequest)
		require.Equal(t, expected.Callsign, actual.Callsign)
		require.InDelta(t, expected.BRA.Bearing().Degrees(), actual.BRA.Bearing().Degrees(), 0.5)
		require.InDelta(t, expected.BRA.Range().NauticalMiles(), actual.BRA.Range().NauticalMiles(), 0.5)
		require.InDelta(t, expected.BRA.Altitude().Feet(), actual.BRA.Altitude().Feet(), 50)
	})
}
