package onvif

// topicInfo describes the ONVIF event topic emitted for motion, and the name
// of the boolean SimpleItem carried in its Data. UniFi Protect's third-party
// ONVIF motion path is undocumented as to which of these it keys on, so the
// topic is switchable via ONVIF_MOTION_TOPIC (see motion-onvif.md).
type topicInfo struct {
	// Topic is the dot-path used in wsnt:Topic content, e.g.
	// "tns1:RuleEngine/CellMotionDetector/Motion".
	Topic string
	// SimpleItemName is the Data SimpleItem Name carrying the boolean state,
	// e.g. "IsMotion" or "State".
	SimpleItemName string
	// TopicSetFragment is the <wsnt:TopicSet> inner XML advertised by
	// GetEventProperties for this topic.
	TopicSetFragment string
}

var cellMotionTopic = topicInfo{
	Topic:          "tns1:RuleEngine/CellMotionDetector/Motion",
	SimpleItemName: "IsMotion",
	TopicSetFragment: `<tns1:RuleEngine><tns1:CellMotionDetector><tns1:Motion wstop:topic="true">` +
		`<tt:MessageDescription IsProperty="true">` +
		`<tt:Source><tt:SimpleItemDescription Name="Source" Type="tt:ReferenceToken"/></tt:Source>` +
		`<tt:Data><tt:SimpleItemDescription Name="IsMotion" Type="xsd:boolean"/></tt:Data>` +
		`</tt:MessageDescription></tns1:Motion></tns1:CellMotionDetector></tns1:RuleEngine>`,
}

var alarmMotionTopic = topicInfo{
	Topic:          "tns1:VideoSource/MotionAlarm",
	SimpleItemName: "State",
	TopicSetFragment: `<tns1:VideoSource><tns1:MotionAlarm wstop:topic="true">` +
		`<tt:MessageDescription IsProperty="true">` +
		`<tt:Source><tt:SimpleItemDescription Name="Source" Type="tt:ReferenceToken"/></tt:Source>` +
		`<tt:Data><tt:SimpleItemDescription Name="State" Type="xsd:boolean"/></tt:Data>` +
		`</tt:MessageDescription></tns1:MotionAlarm></tns1:VideoSource>`,
}

// topicForMode returns the topicInfo for the given ONVIF_MOTION_TOPIC mode
// ("cell" or "alarm"). Any other/empty value falls back to "cell".
func topicForMode(mode string) topicInfo {
	if mode == "alarm" {
		return alarmMotionTopic
	}
	return cellMotionTopic
}
