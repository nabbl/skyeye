package radar

import (
	"math"

	"github.com/dharmab/skyeye/internal/conf"
	"github.com/dharmab/skyeye/pkg/bearings"
	"github.com/dharmab/skyeye/pkg/brevity"
	"github.com/dharmab/skyeye/pkg/coalitions"
	"github.com/dharmab/skyeye/pkg/trackfiles"
	"github.com/martinlindhe/unit"
	"github.com/paulmach/orb"
	"github.com/paulmach/orb/geo"
	"github.com/paulmach/orb/planar"
	"github.com/rs/zerolog/log"
)

// FindNearestTrackfile implements [Radar.FindNearestTrackfile]
func (s *scope) FindNearestTrackfile(
	origin orb.Point,
	minAltitude unit.Length,
	maxAltitude unit.Length,
	radius unit.Length,
	coalition coalitions.Coalition,
	filter brevity.ContactCategory,
) *trackfiles.Trackfile {
	var nearestTrackfile *trackfiles.Trackfile
	nearestDistance := radius
	itr := s.contacts.itr()
	for itr.next() {
		trackfile := itr.value()
		altitude := trackfile.LastKnown().Altitude
		if s.isMatch(trackfile, coalition, filter) && minAltitude <= altitude && altitude <= maxAltitude {
			distance := unit.Length(math.Abs(geo.Distance(origin, trackfile.LastKnown().Point)))
			isNearer := distance < nearestDistance
			if isNearer {
				log.Debug().
					Interface("origin", origin).
					Int("distance", int(distance.NauticalMiles())).
					Str("aircraft", trackfile.Contact.ACMIName).
					Float64("altitude", altitude.Feet()).
					Int("unitID", int(trackfile.Contact.UnitID)).
					Str("name", trackfile.Contact.Name).
					Msg("new candidate for nearest trackfile")
				nearestTrackfile = trackfile
				nearestDistance = distance
			}
		}
	}
	if nearestTrackfile != nil {
		log.Debug().
			Any("origin", origin).
			Str("aircraft", nearestTrackfile.Contact.ACMIName).
			Int("unitID", int(nearestTrackfile.Contact.UnitID)).
			Int("altitude", int(nearestTrackfile.LastKnown().Altitude.Feet())).
			Msg("found nearest contact")
	} else {
		log.Debug().Msg("no contacts found within search volume")
	}
	return nearestTrackfile
}

// FindNearestGroupWithBRAA implements [Radar.FindNearestGroupWithBRAA]
func (s *scope) FindNearestGroupWithBRAA(
	origin orb.Point,
	minAltitude unit.Length,
	maxAltitude unit.Length,
	radius unit.Length,
	coalition coalitions.Coalition,
	filter brevity.ContactCategory,
) brevity.Group {
	trackfile := s.FindNearestTrackfile(origin, minAltitude, maxAltitude, radius, coalition, filter)
	if trackfile == nil {
		return nil
	}

	group := s.findGroupForAircraft(trackfile)
	if group == nil {
		return nil
	}

	declination := s.Declination(origin)
	bearing := bearings.NewTrueBearing(
		unit.Angle(
			geo.Bearing(origin, group.point()),
		) * unit.Degree,
	)
	magBearing := bearing.Magnetic(declination)
	log.Debug().Float64("tru", bearing.Degrees()).Float64("dec", declination.Degrees()).Float64("mag", magBearing.Degrees()).Msg("determined bearing")
	_range := unit.Length(geo.Distance(origin, group.point())) * unit.Meter
	altitude := trackfile.LastKnown().Altitude
	aspect := brevity.AspectFromAngle(magBearing, trackfile.Course())
	group.braa = brevity.NewBRAA(
		magBearing,
		_range,
		altitude,
		aspect,
	)
	group.bullseye = nil
	group.aspect = &aspect
	group.isThreat = _range < brevity.MandatoryThreatDistance

	return group
}

// FindNearestGroupWithBullseye implements [Radar.FindNearestGroupWithBullseye]
func (s *scope) FindNearestGroupWithBullseye(origin orb.Point, minAltitude, maxAltitude, radius unit.Length, coalition coalitions.Coalition, filter brevity.ContactCategory) brevity.Group {
	nearestTrackfile := s.FindNearestTrackfile(origin, minAltitude, maxAltitude, radius, coalition, filter)
	group := s.findGroupForAircraft(nearestTrackfile)
	groupLocation := nearestTrackfile.LastKnown().Point
	aspect := brevity.AspectFromAngle(
		bearings.NewTrueBearing(
			unit.Angle(
				geo.Bearing(origin, groupLocation),
			)*unit.Degree,
		).Magnetic(s.Declination(origin)), nearestTrackfile.Course(),
	)

	group.aspect = &aspect
	rang := unit.Length(geo.Distance(origin, groupLocation)) * unit.Meter
	group.isThreat = rang < brevity.MandatoryThreatDistance
	log.Debug().Interface("origin", origin).Interface("group", group).Msg("determined nearest group")
	return group
}

// FindNearestGroupInSector implements [Radar.FindNearestGroupInSector]
func (s *scope) FindNearestGroupInSector(origin orb.Point, minAltitude, maxAltitude, length unit.Length, bearing bearings.Bearing, arc unit.Angle, coalition coalitions.Coalition, filter brevity.ContactCategory) brevity.Group {
	logger := log.With().Interface("origin", origin).Float64("bearing", bearing.Degrees()).Float64("arc", arc.Degrees()).Logger()

	declination := s.Declination(origin)
	bearing = bearing.Magnetic(declination)

	ring := orb.Ring{origin}
	for a := arc / 2; a > -arc/2; a -= arc / 10 {
		ring = append(
			ring,
			geo.PointAtBearingAndDistance(
				origin,
				(bearing.Value()+a).Degrees(),
				length.Meters(),
			),
		)
	}
	ring = append(ring, origin)
	sector := orb.Polygon{ring}

	logger.Debug().Any("sector", sector).Msg("searching sector")
	nearestDistance := unit.Length(math.MaxFloat64)
	var nearestContact *trackfiles.Trackfile
	itr := s.contacts.itr()
	for itr.next() {
		trackfile := itr.value()
		logger := logger.With().Int("unitID", int(trackfile.Contact.UnitID)).Logger()
		isMatch := s.isMatch(trackfile, coalition, filter)
		isWithinAltitude := minAltitude <= trackfile.LastKnown().Altitude && trackfile.LastKnown().Altitude <= maxAltitude
		if isMatch && isWithinAltitude {
			contactLocation := trackfile.LastKnown().Point
			distanceToContact := unit.Length(geo.Distance(origin, contactLocation)) * unit.Meter
			inSector := planar.PolygonContains(sector, contactLocation)
			logger.Debug().Float64("distanceNM", distanceToContact.NauticalMiles()).Bool("isWithinCone", inSector).Msg("checking distance and location")
			if distanceToContact < nearestDistance && distanceToContact > conf.DefaultMarginRadius && inSector {
				nearestContact = trackfile
			}
		}
	}
	if nearestContact == nil {
		log.Debug().Msg("no contacts found in cone")
		return nil
	}

	logger = log.With().Int("unitID", int(nearestContact.Contact.UnitID)).Logger()
	logger.Debug().Msg("found nearest contact")
	group := s.findGroupForAircraft(nearestContact)
	if group == nil {
		return nil
	}
	preciseBearing := bearings.NewTrueBearing(
		unit.Angle(
			geo.Bearing(origin, nearestContact.LastKnown().Point),
		) * unit.Degree,
	).Magnetic(declination)
	aspect := brevity.AspectFromAngle(preciseBearing, nearestContact.Course())
	log.Debug().Str("aspect", string(aspect)).Msg("determined aspect")
	_range := unit.Length(geo.Distance(origin, nearestContact.LastKnown().Point)) * unit.Meter
	group.aspect = &aspect
	group.braa = brevity.NewBRAA(
		preciseBearing,
		_range,
		group.Altitude(),
		group.Aspect(),
	)
	logger.Debug().Interface("group", group).Msg("determined nearest group")
	group.bullseye = nil
	return group
}
