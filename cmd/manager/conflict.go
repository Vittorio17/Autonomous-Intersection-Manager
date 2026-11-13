package main

import (
	pb "github.com/Vittorio17/Autonomous-Intersection-Manager/proto"
)

// quadrantMask use 4 bit to represent the 4 quadrants of the intersection.
type quadrantMask uint8

const (
	qNone quadrantMask = 0
	qNW   quadrantMask = 1 << iota // 1 (0001) - Nord-Ovest
	qNE                            // 2 (0010) - Nord-Est
	qSW                            // 4 (0100) - Sud-Ovest
	qSE                            // 8 (1000) - Sud-Est
)

// trajectory describes the physical bulk of a maneuver.
type trajectory struct {
	exitLane  pb.Lane
	quadrants quadrantMask
}

// pathKey uniquely identifies a vehicle's maneuver.
type pathKey struct {
	lane pb.Lane
	dir  pb.Direction
}

// trajectoryTable is the static lookup table of the 12 possible maneuvers.
var trajectoryTable = map[pathKey]trajectory{
	// NORTH
	{pb.Lane_LANE_NORTH, pb.Direction_DIR_RIGHT}:    {exitLane: pb.Lane_LANE_WEST, quadrants: qNW},
	{pb.Lane_LANE_NORTH, pb.Direction_DIR_STRAIGHT}: {exitLane: pb.Lane_LANE_SOUTH, quadrants: qNW | qSW},
	{pb.Lane_LANE_NORTH, pb.Direction_DIR_LEFT}:     {exitLane: pb.Lane_LANE_EAST, quadrants: qNW | qSW | qSE},

	// SOUTH
	{pb.Lane_LANE_SOUTH, pb.Direction_DIR_RIGHT}:    {exitLane: pb.Lane_LANE_EAST, quadrants: qSE},
	{pb.Lane_LANE_SOUTH, pb.Direction_DIR_STRAIGHT}: {exitLane: pb.Lane_LANE_NORTH, quadrants: qSE | qNE},
	{pb.Lane_LANE_SOUTH, pb.Direction_DIR_LEFT}:     {exitLane: pb.Lane_LANE_WEST, quadrants: qSE | qNE | qNW},

	// EAST
	{pb.Lane_LANE_EAST, pb.Direction_DIR_RIGHT}:    {exitLane: pb.Lane_LANE_NORTH, quadrants: qNE},
	{pb.Lane_LANE_EAST, pb.Direction_DIR_STRAIGHT}: {exitLane: pb.Lane_LANE_WEST, quadrants: qNE | qNW},
	{pb.Lane_LANE_EAST, pb.Direction_DIR_LEFT}:     {exitLane: pb.Lane_LANE_SOUTH, quadrants: qNE | qNW | qSW},

	// WEST
	{pb.Lane_LANE_WEST, pb.Direction_DIR_RIGHT}:    {exitLane: pb.Lane_LANE_SOUTH, quadrants: qSW},
	{pb.Lane_LANE_WEST, pb.Direction_DIR_STRAIGHT}: {exitLane: pb.Lane_LANE_EAST, quadrants: qSW | qSE},
	{pb.Lane_LANE_WEST, pb.Direction_DIR_LEFT}:     {exitLane: pb.Lane_LANE_NORTH, quadrants: qSW | qSE | qNE},
}

func arePathsConflicting(l1 pb.Lane, d1 pb.Direction, l2 pb.Lane, d2 pb.Direction) bool {
	// Same origin lane
	if l1 == l2 {
		return true
	}

	// Trajectory recovery
	t1, ok1 := trajectoryTable[pathKey{lane: l1, dir: d1}]
	t2, ok2 := trajectoryTable[pathKey{lane: l2, dir: d2}]

	if !ok1 || !ok2 {
		return true
	}

	// Same exit lane
	if t1.exitLane == t2.exitLane {
		return true
	}

	// AND bit by bit (&): if the result != 0, they share at least one quadrant
	return (t1.quadrants & t2.quadrants) != qNone
}