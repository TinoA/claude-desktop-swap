package cmd

import (
	"bytes"
	"errors"
	"reflect"
	"testing"
)

func TestSaveProfileStopsCheckpointsAndRelaunchesWhenRunning(t *testing.T) {
	events := []string{}
	store := &fakeSwitchStore{events: &events}
	p := &fakePlatform{events: &events, appData: t.TempDir(), running: true}
	if err := saveProfileWith("work", store, p, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	want := []string{"app-data", "stop", "checkpoint:work", "launch"}
	if !reflect.DeepEqual(events, want) {
		t.Fatalf("events = %v, want %v", events, want)
	}
}

func TestSaveProfileCheckpointsWithoutTouchingAppWhenStopped(t *testing.T) {
	events := []string{}
	store := &fakeSwitchStore{events: &events}
	p := &fakePlatform{events: &events, appData: t.TempDir(), running: false}
	if err := saveProfileWith("work", store, p, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	want := []string{"app-data", "checkpoint:work"}
	if !reflect.DeepEqual(events, want) {
		t.Fatalf("events = %v, want %v", events, want)
	}
}

func TestSaveProfileRelauchesAfterCheckpointFailure(t *testing.T) {
	events := []string{}
	store := &fakeSwitchStore{events: &events, checkpointErr: errors.New("checkpoint failed")}
	p := &fakePlatform{events: &events, appData: t.TempDir(), running: true}
	if err := saveProfileWith("work", store, p, &bytes.Buffer{}); err == nil {
		t.Fatal("save should fail")
	}
	want := []string{"app-data", "stop", "checkpoint:work", "launch"}
	if !reflect.DeepEqual(events, want) {
		t.Fatalf("events = %v, want %v", events, want)
	}
}
