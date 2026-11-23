package bloomrules

import "strconv"

const templatesPerTier = 50

var metricFamilies = [...][4]Metric{
	{ProductiveRequests, ProductiveRequests, ProductiveRequests, ProductiveRequests},
	{OutputTokens, Images, ProductiveRequests, OutputTokens},
	{ProductiveRequests, Images, ProductiveRequests, Images},
	{OutputTokens, ProductiveRequests, ProductiveRequests, OutputTokens},
	{ProductiveRequests, Images, ProductiveRequests, ProductiveRequests},
}

// Templates returns a fresh copy of the tier's rotation in stable ID order.
func Templates(tier Tier) []Template {
	switch tier {
	case Easy, Medium, Hard, Weekly:
	default:
		return nil
	}
	templates := make([]Template, templatesPerTier)
	for index := range templates {
		templates[index] = templateAt(tier, int64(index))
	}
	return templates
}

func templateAt(tier Tier, index int64) Template {
	family := metricFamilies[index/10]
	if tier == Easy && index == 0 {
		family = [4]Metric{CheckIn, CheckIn, CheckIn, CheckIn}
	}
	if tier == Weekly && index/10 == 4 {
		family = [4]Metric{ActiveDays, ActiveDays, ActiveDays, ActiveDays}
	}
	return Template{
		ID:          string(tier) + "-" + strconv.FormatInt(index+1, 10),
		Tier:        tier,
		Metric:      family[0],
		ImageMetric: family[1],
		ColdMetric:  family[2],
		MixedMetric: family[3],
		Intensity:   index % 10,
	}
}

func (template Template) MetricFor(modality Modality) Metric {
	switch modality {
	case Text:
		return template.Metric
	case Image:
		return template.ImageMetric
	case Mixed:
		return template.MixedMetric
	default:
		return template.ColdMetric
	}
}

func targetFor(tier Tier, metric Metric, intensity int64, history Stats, baselineDays int64) int64 {
	if metric == CheckIn {
		return 1
	}
	var units, scale int64
	switch metric {
	case ProductiveRequests:
		units, scale = history.ProductiveRequests, 1
	case OutputTokens:
		units, scale = history.OutputTokens, 100
	case Images:
		units, scale = history.Images, 1
	case ActiveDays:
		units, scale = history.ActiveDays, 1
	}
	var minimum, rate, variant int64
	switch tier {
	case Easy:
		minimum, rate, variant = 1, 25, intensity/5
	case Medium:
		minimum, rate, variant = 2, 50, intensity/5
	case Hard:
		minimum, rate, variant = 4, 80, intensity/3
	case Weekly:
		minimum, rate, variant = 4, 65, intensity/2
	}
	rate += intensity * 2
	if tier == Weekly {
		if metric == ActiveDays {
			minimum, variant = 2, intensity/4
		}
	} else {
		units /= baselineDays
	}
	target := add(max(minimum*scale, percent(max(0, units), rate)), variant*scale)
	if metric == ActiveDays {
		return min(target, 7)
	}
	return target
}
