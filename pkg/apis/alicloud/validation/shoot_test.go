// SPDX-FileCopyrightText: 2024 SAP SE or an SAP affiliate company and Gardener contributors
//
// SPDX-License-Identifier: Apache-2.0

package validation_test

import (
	"github.com/gardener/gardener/pkg/apis/core"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	. "github.com/onsi/gomega/gstruct"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/validation/field"
	"k8s.io/utils/ptr"

	apisalicloud "github.com/gardener/gardener-extension-provider-alicloud/pkg/apis/alicloud"
	. "github.com/gardener/gardener-extension-provider-alicloud/pkg/apis/alicloud/validation"
)

var _ = Describe("Shoot validation", func() {
	Describe("#ValidateNetworking", func() {
		var networkingPath = field.NewPath("spec", "networking")

		It("should return no error because network settings are correct", func() {
			networking := &core.Networking{
				Nodes:    ptr.To("10.252.0.0/16"),
				Pods:     ptr.To("192.168.0.0/16"),
				Services: ptr.To("172.16.0.0/16"),
			}

			errorList := ValidateNetworking(networking, networkingPath)
			Expect(errorList).To(BeEmpty())
		})

		It("should return errors because CIDR overlaps with 100.64.0.0/10", func() {
			networking := &core.Networking{
				Nodes:    ptr.To("100.100.0.0/16"),
				Pods:     ptr.To("100.101.0.0/16"),
				Services: ptr.To("100.102.0.0/16"),
			}
			errorList := ValidateNetworking(networking, networkingPath)

			Expect(errorList).To(ConsistOf(
				PointTo(MatchFields(IgnoreExtras, Fields{
					"Type":     Equal(field.ErrorTypeInvalid),
					"Field":    Equal("spec.networking.nodes"),
					"BadValue": Equal("100.100.0.0/16"),
				})),
				PointTo(MatchFields(IgnoreExtras, Fields{
					"Type":     Equal(field.ErrorTypeInvalid),
					"Field":    Equal("spec.networking.pods"),
					"BadValue": Equal("100.101.0.0/16"),
				})),
				PointTo(MatchFields(IgnoreExtras, Fields{
					"Type":     Equal(field.ErrorTypeInvalid),
					"Field":    Equal("spec.networking.services"),
					"BadValue": Equal("100.102.0.0/16"),
				})),
			))
		})

		It("should return errors because nodes' CIDR is nil", func() {
			networking := &core.Networking{
				Nodes:    nil,
				Pods:     nil,
				Services: nil,
			}

			errorList := ValidateNetworking(networking, networkingPath)
			Expect(errorList).To(ConsistOf(
				PointTo(MatchFields(IgnoreExtras, Fields{
					"Type":  Equal(field.ErrorTypeRequired),
					"Field": Equal("spec.networking.nodes"),
				})),
			))
		})

		It("should forbid updating validated networking CIDR", func() {
			oldNetworking := &core.Networking{
				Nodes:    ptr.To("10.252.0.0/16"),
				Pods:     ptr.To("192.168.0.0/16"),
				Services: ptr.To("172.16.0.0/16"),
			}

			newNetworking := &core.Networking{
				Nodes:    ptr.To("10.250.0.0/16"),
				Pods:     ptr.To("192.168.0.0/16"),
				Services: ptr.To("172.17.0.0/16"),
			}

			errorList := ValidateNetworkingUpdate(oldNetworking, newNetworking, networkingPath)
			Expect(errorList).To(ConsistOf(
				PointTo(MatchFields(IgnoreExtras, Fields{
					"Type":  Equal(field.ErrorTypeInvalid),
					"Field": Equal("spec.networking.nodes"),
				})),
				PointTo(MatchFields(IgnoreExtras, Fields{
					"Type":  Equal(field.ErrorTypeInvalid),
					"Field": Equal("spec.networking.services"),
				})),
			))
		})

		It("should allow updating invalidated networking CIDR", func() {
			oldNetworking := &core.Networking{
				Nodes:    ptr.To("null"),
				Pods:     ptr.To("null"),
				Services: ptr.To("null"),
			}

			newNetworking := &core.Networking{
				Nodes:    ptr.To("10.250.0.0/16"),
				Pods:     ptr.To("192.168.0.0/16"),
				Services: ptr.To("172.17.0.0/16"),
			}

			errorList := ValidateNetworkingUpdate(oldNetworking, newNetworking, networkingPath)
			Expect(errorList).To(BeEmpty())
		})
	})

	Describe("#ValidateWorkerConfig", func() {
		var (
			workers       []core.Worker
			alicloudZones []apisalicloud.Zone
		)

		BeforeEach(func() {
			workers = []core.Worker{
				{
					Name: "worker1",
					Volume: &core.Volume{
						Type:       ptr.To("Volume"),
						VolumeSize: "30G",
					},
					Zones: []string{
						"zone1",
						"zone2",
					},
				},
				{
					Name: "worker2",
					Volume: &core.Volume{
						Type:       ptr.To("Volume"),
						VolumeSize: "20G",
					},
					Zones: []string{
						"zone2",
						"zone3",
					},
				},
			}

			alicloudZones = []apisalicloud.Zone{
				{
					Name:    "zone1",
					Workers: "1.2.3.4/5",
				},
				{
					Name:    "zone2",
					Workers: "1.2.3.4/5",
				},
				{
					Name:    "zone3",
					Workers: "1.2.3.4/5",
				},
			}
		})

		Describe("#ValidateWorkers", func() {
			It("should pass because workers are configured correctly", func() {
				errorList := ValidateWorkers(workers, alicloudZones, field.NewPath(""))

				Expect(errorList).To(BeEmpty())
			})

			It("should forbid because volume is not configured", func() {
				workers[0].Volume = nil

				errorList := ValidateWorkers(workers, alicloudZones, field.NewPath("workers"))

				Expect(errorList).To(ConsistOf(
					PointTo(MatchFields(IgnoreExtras, Fields{
						"Type":  Equal(field.ErrorTypeRequired),
						"Field": Equal("workers[0].volume"),
					})),
				))
			})

			It("should forbid because volume type and size are not configured", func() {
				workers[0].Volume.Type = nil
				workers[0].Volume.VolumeSize = ""
				workers[0].Volume.Encrypted = ptr.To(false)
				workers[0].DataVolumes = []core.DataVolume{
					{},
					{Name: "too-long-data-volume-name-exceeding-the-maximum-limit-of-64-charts", VolumeSize: "24Gi", Type: ptr.To("some-type")},
					{Name: "regex/fails", VolumeSize: "24Gi", Type: ptr.To("some-type")},
				}

				errorList := ValidateWorkers(workers, alicloudZones, field.NewPath("workers"))

				Expect(errorList).To(ConsistOf(
					PointTo(MatchFields(IgnoreExtras, Fields{
						"Type":  Equal(field.ErrorTypeRequired),
						"Field": Equal("workers[0].volume.type"),
					})),
					PointTo(MatchFields(IgnoreExtras, Fields{
						"Type":  Equal(field.ErrorTypeRequired),
						"Field": Equal("workers[0].volume.size"),
					})),
					PointTo(MatchFields(IgnoreExtras, Fields{
						"Type":  Equal(field.ErrorTypeInvalid),
						"Field": Equal("workers[0].dataVolumes[0].name"),
					})),
					PointTo(MatchFields(IgnoreExtras, Fields{
						"Type":  Equal(field.ErrorTypeRequired),
						"Field": Equal("workers[0].dataVolumes[0].type"),
					})),
					PointTo(MatchFields(IgnoreExtras, Fields{
						"Type":  Equal(field.ErrorTypeRequired),
						"Field": Equal("workers[0].dataVolumes[0].size"),
					})),
					PointTo(MatchFields(IgnoreExtras, Fields{
						"Type":  Equal(field.ErrorTypeTooLong),
						"Field": Equal("workers[0].dataVolumes[1].name"),
					})),
					PointTo(MatchFields(IgnoreExtras, Fields{
						"Type":  Equal(field.ErrorTypeInvalid),
						"Field": Equal("workers[0].dataVolumes[2].name"),
					})),
				))
			})

			It("should forbid because worker does not specify a zone", func() {
				workers[0].Zones = nil

				errorList := ValidateWorkers(workers, alicloudZones, field.NewPath("workers"))

				Expect(errorList).To(ConsistOf(
					PointTo(MatchFields(IgnoreExtras, Fields{
						"Type":  Equal(field.ErrorTypeRequired),
						"Field": Equal("workers[0].zones"),
					})),
				))
			})

			It("should forbid because worker use zones which are not available", func() {
				workers[0].Zones[0] = "zone4"
				workers[1].Zones[1] = "not-available"

				errorList := ValidateWorkers(workers, alicloudZones, field.NewPath("workers"))

				Expect(errorList).To(ConsistOf(
					PointTo(MatchFields(IgnoreExtras, Fields{
						"Type":  Equal(field.ErrorTypeInvalid),
						"Field": Equal("workers[0].zones[0]"),
					})),
					PointTo(MatchFields(IgnoreExtras, Fields{
						"Type":  Equal(field.ErrorTypeInvalid),
						"Field": Equal("workers[1].zones[1]"),
					})),
				))
			})

			It("should pass because worker setting maximum = 0 and minimum = 0", func() {
				workers[0].Maximum = 0
				workers[0].Minimum = 0

				errorList := ValidateWorkers(workers, alicloudZones, field.NewPath("workers"))

				Expect(errorList).To(BeEmpty())
			})
		})

		Describe("#ValidateWorkersUpdate", func() {
			It("should pass because workers are unchanged", func() {
				newWorkers := copyWorkers(workers)

				errorList := ValidateWorkersUpdate(workers, newWorkers, field.NewPath("workers"))

				Expect(errorList).To(BeEmpty())
			})

			It("should allow adding workers", func() {
				newWorkers := copyWorkers(workers)
				newWorkers = append(newWorkers, core.Worker{Name: "worker3", Zones: []string{"zone1"}})

				errorList := ValidateWorkersUpdate(workers, newWorkers, field.NewPath("workers"))

				Expect(errorList).To(BeEmpty())
			})

			It("should allow adding a zone to a worker", func() {
				newWorkers := copyWorkers(workers)
				newWorkers[0].Zones = append(newWorkers[0].Zones, "another-zone")

				errorList := ValidateWorkersUpdate(workers, newWorkers, field.NewPath("workers"))

				Expect(errorList).To(BeEmpty())
			})

			It("should forbid removing a zone from a worker", func() {
				newWorkers := copyWorkers(workers)
				newWorkers[1].Zones = newWorkers[1].Zones[1:]

				errorList := ValidateWorkersUpdate(workers, newWorkers, field.NewPath("workers"))

				Expect(errorList).To(ConsistOf(
					PointTo(MatchFields(IgnoreExtras, Fields{
						"Type":  Equal(field.ErrorTypeInvalid),
						"Field": Equal("workers[1].zones"),
					})),
				))
			})

			It("should forbid changing the zone order", func() {
				newWorkers := copyWorkers(workers)
				newWorkers[0].Zones[0] = workers[0].Zones[1]
				newWorkers[0].Zones[1] = workers[0].Zones[0]
				newWorkers[1].Zones[0] = workers[1].Zones[1]
				newWorkers[1].Zones[1] = workers[1].Zones[0]

				errorList := ValidateWorkersUpdate(workers, newWorkers, field.NewPath("workers"))

				Expect(errorList).To(ConsistOf(
					PointTo(MatchFields(IgnoreExtras, Fields{
						"Type":  Equal(field.ErrorTypeInvalid),
						"Field": Equal("workers[0].zones"),
					})),
					PointTo(MatchFields(IgnoreExtras, Fields{
						"Type":  Equal(field.ErrorTypeInvalid),
						"Field": Equal("workers[1].zones"),
					})),
				))
			})

			It("should forbid changing the providerConfig when update strategy is in-place", func() {
				workers[0].UpdateStrategy = ptr.To(core.AutoInPlaceUpdate)
				workers[0].ProviderConfig = &runtime.RawExtension{
					Raw: []byte(`{"prov1":"val1"}`),
				}

				workers[1].UpdateStrategy = ptr.To(core.ManualInPlaceUpdate)
				workers[1].ProviderConfig = &runtime.RawExtension{
					Raw: []byte(`{"prov2":"val2"}`),
				}

				workers = append(workers, core.Worker{
					Name:           "worker3",
					UpdateStrategy: ptr.To(core.AutoRollingUpdate),
					ProviderConfig: &runtime.RawExtension{
						Raw: []byte(`{"prov3":"val3"}`),
					},
				})

				workers = append(workers, core.Worker{
					Name:           "worker4",
					UpdateStrategy: ptr.To(core.AutoInPlaceUpdate),
					ProviderConfig: &runtime.RawExtension{
						Raw: []byte(`{"prov4":"val4"}`),
					},
				})

				newWorkers := copyWorkers(workers)
				newWorkers[0].ProviderConfig = &runtime.RawExtension{
					Raw: []byte(`{"prov1":"newval1"}`),
				}
				newWorkers[1].ProviderConfig = &runtime.RawExtension{
					Raw: []byte(`{"prov2":"newval2"}`),
				}
				newWorkers[2].ProviderConfig = &runtime.RawExtension{
					Raw: []byte(`{"prov3":"newval3"}`),
				}

				errorList := ValidateWorkersUpdate(workers, newWorkers, field.NewPath("workers"))

				Expect(errorList).To(ConsistOf(
					PointTo(MatchFields(IgnoreExtras, Fields{
						"Type":   Equal(field.ErrorTypeInvalid),
						"Field":  Equal("workers[0].providerConfig"),
						"Detail": Equal("providerConfig is immutable when update strategy is in-place"),
					})),
					PointTo(MatchFields(IgnoreExtras, Fields{
						"Type":   Equal(field.ErrorTypeInvalid),
						"Field":  Equal("workers[1].providerConfig"),
						"Detail": Equal("providerConfig is immutable when update strategy is in-place"),
					})),
				))
			})

			It("should forbid changing dataVolumes when update strategy is in-place", func() {
				workers[0].UpdateStrategy = ptr.To(core.AutoInPlaceUpdate)
				workers[0].DataVolumes = []core.DataVolume{
					{
						Name:       "volume1",
						VolumeSize: "10Gi",
					},
				}

				workers[1].UpdateStrategy = ptr.To(core.ManualInPlaceUpdate)
				workers[1].DataVolumes = []core.DataVolume{
					{
						Name:       "volume2",
						VolumeSize: "20Gi",
					},
				}

				workers = append(workers, core.Worker{
					Name:           "worker3",
					UpdateStrategy: ptr.To(core.AutoRollingUpdate),
					DataVolumes: []core.DataVolume{
						{
							Name:       "volume3",
							VolumeSize: "30Gi",
						},
					},
				})
				workers = append(workers, core.Worker{
					Name:           "worker4",
					UpdateStrategy: ptr.To(core.AutoInPlaceUpdate),
					DataVolumes: []core.DataVolume{
						{
							Name:       "volume4",
							VolumeSize: "40Gi",
						},
					},
				})

				newWorkers := copyWorkers(workers)
				newWorkers[0].DataVolumes = []core.DataVolume{
					{
						Name:       "volume1",
						VolumeSize: "15Gi",
					},
				}
				newWorkers[1].DataVolumes = []core.DataVolume{
					{
						Name:       "volume2",
						VolumeSize: "25Gi",
					},
				}
				newWorkers[2].DataVolumes = []core.DataVolume{
					{
						Name:       "volume3",
						VolumeSize: "35Gi",
					},
				}

				errorList := ValidateWorkersUpdate(workers, newWorkers, field.NewPath("workers"))

				Expect(errorList).To(ConsistOf(
					PointTo(MatchFields(IgnoreExtras, Fields{
						"Type":   Equal(field.ErrorTypeInvalid),
						"Field":  Equal("workers[0].dataVolumes"),
						"Detail": Equal("dataVolumes is immutable when update strategy is in-place"),
					})),
					PointTo(MatchFields(IgnoreExtras, Fields{
						"Type":   Equal(field.ErrorTypeInvalid),
						"Field":  Equal("workers[1].dataVolumes"),
						"Detail": Equal("dataVolumes is immutable when update strategy is in-place"),
					})),
				))
			})

		})

		It("should forbid adding a zone while changing an existing one", func() {
			newWorkers := copyWorkers(workers)
			newWorkers = append(newWorkers, core.Worker{Name: "worker3", Zones: []string{"zone1"}})
			newWorkers[1].Zones[0] = workers[1].Zones[1]

			errorList := ValidateWorkersUpdate(workers, newWorkers, field.NewPath("workers"))

			Expect(errorList).To(ConsistOf(
				PointTo(MatchFields(IgnoreExtras, Fields{
					"Type":  Equal(field.ErrorTypeInvalid),
					"Field": Equal("workers[1].zones"),
				})),
			))
		})
	})
})

func copyWorkers(workers []core.Worker) []core.Worker {
	cp := append(workers[:0:0], workers...)
	for i := range cp {
		cp[i].Zones = append(workers[i].Zones[:0:0], workers[i].Zones...)
	}
	return cp
}
