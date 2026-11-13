package main

import (
	"testing"

	pb "github.com/Vittorio17/Autonomous-Intersection-Manager/proto"
)

func TestArePathsConflicting(t *testing.T) {
	
	tests := []struct {
		name     string
		l1       pb.Lane
		d1       pb.Direction
		l2       pb.Lane
		d2       pb.Direction
		expected bool
	}{
		{
			name:     "Same Lane, Same Direction",
			l1:       pb.Lane_LANE_NORTH,
			d1:       pb.Direction_DIR_STRAIGHT,
			l2:       pb.Lane_LANE_NORTH,
			d2:       pb.Direction_DIR_STRAIGHT,
			expected: true,
		},
		{
			name:     "Same Lane, Different Direction",
			l1:       pb.Lane_LANE_EAST,
			d1:       pb.Direction_DIR_LEFT,
			l2:       pb.Lane_LANE_EAST,
			d2:       pb.Direction_DIR_RIGHT,
			expected: true,
		},
		{
			name:     "North-South straight opposites (parallels)",
			l1:       pb.Lane_LANE_NORTH,
			d1:       pb.Direction_DIR_STRAIGHT,
			l2:       pb.Lane_LANE_SOUTH,
			d2:       pb.Direction_DIR_STRAIGHT,
			expected: false,
		},
		{
			name:     "East-West straight opposites (parallels)",
			l1:       pb.Lane_LANE_EAST,
			d1:       pb.Direction_DIR_STRAIGHT,
			l2:       pb.Lane_LANE_WEST,
			d2:       pb.Direction_DIR_STRAIGHT,
			expected: false,
		},
		{
			name:     "Simultaneous opposite right turns",
			l1:       pb.Lane_LANE_NORTH,
			d1:       pb.Direction_DIR_RIGHT,
			l2:       pb.Lane_LANE_SOUTH,
			d2:       pb.Direction_DIR_RIGHT,
			expected: false,
		},
		{
			name:     "Adjacent right turns at the same time",
			l1:       pb.Lane_LANE_NORTH,
			d1:       pb.Direction_DIR_RIGHT,
			l2:       pb.Lane_LANE_EAST,
			d2:       pb.Direction_DIR_RIGHT,
			expected: false,
		},
		{
			name:     "Turn right vs straight ahead not interfering",
			l1:       pb.Lane_LANE_NORTH,
			d1:       pb.Direction_DIR_RIGHT,
			l2:       pb.Lane_LANE_SOUTH,
			d2:       pb.Direction_DIR_STRAIGHT,
			expected: false,
		},
		{
			name:     "Entry conflict: North->Right (exits West) vs East->Straight (exits West)",
			l1:       pb.Lane_LANE_NORTH,
			d1:       pb.Direction_DIR_RIGHT,
			l2:       pb.Lane_LANE_EAST,
			d2:       pb.Direction_DIR_STRAIGHT,
			expected: true,
		},
		{
			name:     "Turn left cuts straight opposite flow",
			l1:       pb.Lane_LANE_NORTH,
			d1:       pb.Direction_DIR_LEFT,
			l2:       pb.Lane_LANE_SOUTH,
			d2:       pb.Direction_DIR_STRAIGHT,
			expected: true,
		},
		{
			name:     "Straight perpendicular trajectories",
			l1:       pb.Lane_LANE_NORTH,
			d1:       pb.Direction_DIR_STRAIGHT,
			l2:       pb.Lane_LANE_EAST,
			d2:       pb.Direction_DIR_STRAIGHT,
			expected: true,
		},
		{
			name:     "Left turn North->East vs protected right turn East->North",
			l1:       pb.Lane_LANE_NORTH,
			d1:       pb.Direction_DIR_LEFT,
			l2:       pb.Lane_LANE_EAST,
			d2:       pb.Direction_DIR_RIGHT,
			expected: false,
		},
		{
			name:     "Lane enum not specified (fail-safe)",
			l1:       pb.Lane_LANE_UNKNOWN,
			d1:       pb.Direction_DIR_STRAIGHT,
			l2:       pb.Lane_LANE_SOUTH,
			d2:       pb.Direction_DIR_STRAIGHT,
			expected: true,
		},
		{
			name:     "Enum direction not specified (fail-safe)",
			l1:       pb.Lane_LANE_NORTH,
			d1:       pb.Direction_DIR_UNKNOWN,
			l2:       pb.Lane_LANE_SOUTH,
			d2:       pb.Direction_DIR_STRAIGHT,
			expected: true,
		},
	}

	
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := arePathsConflicting(tc.l1, tc.d1, tc.l2, tc.d2)
			if got != tc.expected {
				t.Errorf("arePathsConflicting(%v, %v, %v, %v) = %v; want %v",
					tc.l1, tc.d1, tc.l2, tc.d2, got, tc.expected)
			}

			// Let's also check that the function is symmetric: f(A, B) == f(B, A)
			gotReverse := arePathsConflicting(tc.l2, tc.d2, tc.l1, tc.d1)
			if gotReverse != tc.expected {
				t.Errorf("Symmetry broken for (%v, %v) vs (%v, %v): got %v; want %v",
					tc.l2, tc.d2, tc.l1, tc.d1, gotReverse, tc.expected)
			}
		})
	}
}