package controller

import (
	"github.com/dharmab/skyeye/pkg/bearings"
	"github.com/dharmab/skyeye/pkg/brevity"
	"github.com/martinlindhe/unit"
	"github.com/paulmach/orb"
	"github.com/paulmach/orb/geo"
	"github.com/rs/zerolog/log"
)

// HandleDeclare implements Controller.HandleDeclare.
func (c *controller) HandleDeclare(request *brevity.DeclareRequest) {
	logger := log.With().Str("callsign", request.Callsign).Type("type", request).Logger()
	logger.Debug().Msg("handling request")

	if request.IsBRAA {
		logger = logger.With().
			Float64("bearingDegrees", request.Bearing.Degrees()).
			Float64("rangeNM", request.Range.NauticalMiles()).
			Logger()
	} else {
		logger = logger.With().
			Float64("bearingDegrees", request.Bullseye.Bearing().Degrees()).
			Float64("distanceNM", request.Bullseye.Distance().NauticalMiles()).
			Logger()
	}
	logger = logger.With().
		Bool("isBRAA", request.IsBRAA).
		Float64("altitudeFeet", request.Altitude.Feet()).
		Logger()
	logger.Info().Msg("handling DECLARE request")

	foundCallsign, trackfile := c.scope.FindCallsign(request.Callsign, c.coalition)
	if trackfile == nil {
		logger.Info().Msg("no trackfile found for requestor")
		c.out <- brevity.NegativeRadarContactResponse{Callsign: request.Callsign}
		return
	}

	var origin orb.Point
	var bearing bearings.Bearing
	var distance unit.Length
	if request.IsBRAA {
		logger.Debug().Msg("locating point of interest using BRAA")
		if !request.Bearing.IsMagnetic() {
			logger.Warn().Any("bearing", request.Bearing).Msg("bearing provided to HandleDeclare should be magnetic")
		}
		origin = trackfile.LastKnown().Point
		bearing = request.Bearing
		distance = request.Range
	} else {
		logger.Debug().Msg("locating point of interest using bullseye")
		if request == nil {
			logger.Error().Msg("request is nil")
		} else if request.Bullseye.Bearing() == nil {
			logger.Error().Msg("request.Bullseye.Bearing() is nil")
		}
		if !request.Bullseye.Bearing().IsMagnetic() {
			logger.Warn().Any("bearing", request.Bullseye.Bearing()).Msg("bearing provided to HandleDeclare should be magnetic")
		}
		origin = c.scope.Bullseye(trackfile.Contact.Coalition)
		bearing = request.Bullseye.Bearing().True(c.scope.Declination(origin))
		distance = request.Bullseye.Distance()
	}
	aoi := geo.PointAtBearingAndDistance(origin, bearing.Degrees(), distance.Meters())

	radius := 7 * unit.NauticalMile // TODO reduce to 3 when magvar is available
	altitudeMargin := unit.Length(5000) * unit.Foot
	minAltitude := request.Altitude - altitudeMargin
	maxAltitude := request.Altitude + altitudeMargin
	friendlyGroups := c.scope.FindNearbyGroupsWithBullseye(aoi, minAltitude, maxAltitude, radius, c.coalition, brevity.Aircraft)
	hostileGroups := c.scope.FindNearbyGroupsWithBullseye(aoi, minAltitude, maxAltitude, radius, c.hostileCoalition(), brevity.Aircraft)
	logger.Debug().Int("friendly", len(friendlyGroups)).Int("hostile", len(hostileGroups)).Msg("queried groups near declared location")

	response := brevity.DeclareResponse{Callsign: foundCallsign}
	if len(friendlyGroups)+len(hostileGroups) == 0 {
		logger.Debug().Msg("no groups found")
		response.Declaration = brevity.Clean
	} else if len(friendlyGroups) > 0 && len(hostileGroups) == 0 {
		logger.Debug().Msg("friendly groups found")
		response.Declaration = brevity.Friendly
		response.Group = friendlyGroups[0]
	} else if len(friendlyGroups) == 0 && len(hostileGroups) > 0 {
		logger.Debug().Msg("hostile groups found")
		response.Declaration = brevity.Hostile
		response.Group = hostileGroups[0]
	} else if len(friendlyGroups) > 0 && len(hostileGroups) > 0 {
		logger.Debug().Msg("both friendly and hostile groups found")
		response.Declaration = brevity.Furball
	}

	if response.Group != nil {
		response.Group.SetDeclaration(response.Declaration)
	}

	logger.Debug().Any("declaration", response.Declaration).Msg("responding to DECLARE request")
	c.out <- response
}
