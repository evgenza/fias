package events

import "testing"

func TestObjectsChangedIdentity(t *testing.T) {
	full := ObjectsChanged{Digest: "sha256", RunID: 1, Path: "77/AS_HOUSES.xml"}
	retry := full
	retry.RunID = 2
	if objectsChangedMsgID(full) != objectsChangedMsgID(retry) {
		t.Fatal("run ID changes archive identity")
	}
	sample := full
	sample.SampleLimit = 100
	if objectsChangedMsgID(full) == objectsChangedMsgID(sample) {
		t.Fatal("sample suppresses subsequent full import")
	}
}
