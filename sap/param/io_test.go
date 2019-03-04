package param

import (
	"io/ioutil"
	"path"
	"testing"
)

func TestIOElevators(t *testing.T) {
	inspected, err := BlockDeviceSchedulers{}.Inspect()
	if err != nil {
		t.Fatal(err, inspected)
	}
	if len(inspected.(BlockDeviceSchedulers).SchedulerChoice) == 0 {
		t.Skip("the test case will not continue because inspection result turns out empty")
	}
	for name, elevator := range inspected.(BlockDeviceSchedulers).SchedulerChoice {
		if name == "" || elevator == "" {
			t.Fatal(inspected)
		}
	}
	optimised, err := inspected.Optimise("noop")
	if err != nil {
		t.Fatal(err)
	}
	if len(optimised.(BlockDeviceSchedulers).SchedulerChoice) == 0 {
		t.Fatal(optimised)
	}
	for name, elevator := range optimised.(BlockDeviceSchedulers).SchedulerChoice {
		if name == "" || elevator != "noop" {
			t.Fatal(optimised)
		}
	}
}

func TestNrRequests(t *testing.T) {
	inspected, err := BlockDeviceNrRequests{}.Inspect()
	if err != nil {
		t.Fatal(err, inspected)
	}
	if len(inspected.(BlockDeviceNrRequests).NrRequests) == 0 {
		t.Skip("the test case will not continue because inspection result turns out empty")
	}
	for name, nrrequest := range inspected.(BlockDeviceNrRequests).NrRequests {
		if name == "" || nrrequest < 0 {
			t.Fatal(inspected)
		}
	}
	optimised, err := inspected.Optimise(128)
	if err != nil {
		t.Fatal(err)
	}
	if len(optimised.(BlockDeviceNrRequests).NrRequests) == 0 {
		t.Fatal(optimised)
	}
	for name, nrrequest := range optimised.(BlockDeviceNrRequests).NrRequests {
		if name == "" || nrrequest < 0 {
			t.Fatal(optimised)
		}
	}
}

func TestIsValidScheduler(t *testing.T) {
	scheduler := ""
	dirCont, err := ioutil.ReadDir("/sys/block")
	if err != nil {
		t.Skip("no block files available. Skip test.")
	}
	for _, entry := range dirCont {
		_, err := ioutil.ReadDir(path.Join("/sys/block/", entry.Name(), "mq"))
		if err != nil {
			// single queue scheduler (values: noop deadline cfq)
			scheduler = "cfq"
		} else {
			// multi queue scheduler (values: mq-deadline kyber bfq none)
			scheduler = "none"
		}
		if entry.Name() == "sda" {
			if !IsValidScheduler("sda", scheduler) {
				t.Fatalf("'%s' is not a valid scheduler for 'sda'\n", scheduler)
			}
			if IsValidScheduler("sda", "hugo") {
				t.Fatal("'hugo' is a valid scheduler for 'sda'")
			}
		}
		if entry.Name() == "vda" {
			if !IsValidScheduler("vda", scheduler) {
				t.Fatalf("'%s' is not a valid scheduler for 'vda'\n", scheduler)
			}
			if IsValidScheduler("vda", "hugo") {
				t.Fatal("'hugo' is a valid scheduler for 'vda'")
			}
		}
	}
}

func TestIsValidforNrRequests(t *testing.T) {
	dirCont, err := ioutil.ReadDir("/sys/block")
	if err != nil {
		t.Skip("no block files available. Skip test.")
	}
	for _, entry := range dirCont {
		if entry.Name() == "sda" {
			if !IsValidforNrRequests("sda", "1024") {
				t.Log("'1024' is not a valid number of requests for 'sda'")
			} else {
				t.Log("'1024' is a valid number of requests for 'sda'")
			}
		}
		if entry.Name() == "vda" {
			if !IsValidforNrRequests("vda", "128") {
				t.Log("'128' is not a valid number of requests for 'vda'")
			} else {
				t.Log("'128' is a valid number of requests for 'vda'")
			}
		}
	}
}
