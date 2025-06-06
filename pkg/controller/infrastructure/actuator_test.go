// SPDX-FileCopyrightText: 2024 SAP SE or an SAP affiliate company and Gardener contributors
//
// SPDX-License-Identifier: Apache-2.0

package infrastructure_test

import (
	"context"
	"time"

	"github.com/aliyun/alibaba-cloud-sdk-go/services/vpc"
	"github.com/gardener/gardener/extensions/pkg/controller"
	"github.com/gardener/gardener/extensions/pkg/controller/infrastructure"
	realterraformer "github.com/gardener/gardener/extensions/pkg/terraformer"
	mockterraformer "github.com/gardener/gardener/extensions/pkg/terraformer/mock"
	gardencorev1beta1 "github.com/gardener/gardener/pkg/apis/core/v1beta1"
	extensionsv1alpha1 "github.com/gardener/gardener/pkg/apis/extensions/v1alpha1"
	mockclient "github.com/gardener/gardener/third_party/mock/controller-runtime/client"
	mockmanager "github.com/gardener/gardener/third_party/mock/controller-runtime/manager"
	"github.com/go-logr/logr"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"go.uber.org/mock/gomock"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/serializer/json"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/gardener/gardener-extension-provider-alicloud/imagevector"
	"github.com/gardener/gardener-extension-provider-alicloud/pkg/alicloud"
	alicloudclient "github.com/gardener/gardener-extension-provider-alicloud/pkg/alicloud/client"
	"github.com/gardener/gardener-extension-provider-alicloud/pkg/apis/alicloud/install"
	alicloudv1alpha1 "github.com/gardener/gardener-extension-provider-alicloud/pkg/apis/alicloud/v1alpha1"
	. "github.com/gardener/gardener-extension-provider-alicloud/pkg/controller/infrastructure"
	mockalicloudclient "github.com/gardener/gardener-extension-provider-alicloud/pkg/mock/provider-alicloud/alicloud/client"
	mockinfrastructure "github.com/gardener/gardener-extension-provider-alicloud/pkg/mock/provider-alicloud/controller/infrastructure"
)

func expectEncode(data []byte, err error) []byte {
	Expect(err).NotTo(HaveOccurred())
	Expect(data).NotTo(BeNil())
	return data
}

var _ = Describe("Actuator", func() {
	var (
		ctrl       *gomock.Controller
		scheme     *runtime.Scheme
		serializer runtime.Serializer
	)

	BeforeEach(func() {
		ctrl = gomock.NewController(GinkgoT())
		scheme = runtime.NewScheme()
		install.Install(scheme)
		Expect(controller.AddToScheme(scheme)).To(Succeed())
		serializer = json.NewSerializerWithOptions(json.DefaultMetaFactory, scheme, scheme, json.SerializerOptions{})
	})

	AfterEach(func() {
		ctrl.Finish()
	})

	Context("Actuator", func() {
		var (
			ctx                   context.Context
			alicloudClientFactory *mockalicloudclient.MockClientFactory
			vpcClient             *mockalicloudclient.MockVPC
			terraformerFactory    *mockterraformer.MockFactory
			terraformer           *mockterraformer.MockTerraformer
			shootECSClient        *mockalicloudclient.MockECS
			shootROSClient        *mockalicloudclient.MockROS
			shootSTSClient        *mockalicloudclient.MockSTS
			shootRAMClient        *mockalicloudclient.MockRAM
			terraformChartOps     *mockinfrastructure.MockTerraformChartOps
			actuator              infrastructure.Actuator
			c                     *mockclient.MockClient
			mgr                   *mockmanager.MockManager
			sw                    *mockclient.MockStatusWriter
			initializer           *mockterraformer.MockInitializer
			restConfig            rest.Config
			logger                logr.Logger

			cidr    string
			config  alicloudv1alpha1.InfrastructureConfig
			podCIDR string

			configYAML      []byte
			secretNamespace string
			secretName      string
			region          string
			infra           extensionsv1alpha1.Infrastructure
			owner           *metav1.OwnerReference
			accessKeyID     string
			accessKeySecret string
			credentialsFile string
			cluster         controller.Cluster

			initializerValues InitializerValues
			chartValues       map[string]interface{}

			vpcID           string
			vpcCIDRString   string
			securityGroupID string
			rawState        *realterraformer.RawState
			fakeTfState     *realterraformer.RawState

			serviceForNatGw           string
			serviceLinkedRoleForNatGw string

			err error
		)

		Describe("#Reconcile", func() {
			BeforeEach(func() {
				ctx = context.TODO()
				alicloudClientFactory = mockalicloudclient.NewMockClientFactory(ctrl)
				vpcClient = mockalicloudclient.NewMockVPC(ctrl)
				terraformerFactory = mockterraformer.NewMockFactory(ctrl)
				terraformer = mockterraformer.NewMockTerraformer(ctrl)
				shootECSClient = mockalicloudclient.NewMockECS(ctrl)
				shootSTSClient = mockalicloudclient.NewMockSTS(ctrl)
				shootRAMClient = mockalicloudclient.NewMockRAM(ctrl)
				shootROSClient = mockalicloudclient.NewMockROS(ctrl)
				terraformChartOps = mockinfrastructure.NewMockTerraformChartOps(ctrl)
				logger = log.Log.WithName("test")

				c = mockclient.NewMockClient(ctrl)
				sw = mockclient.NewMockStatusWriter(ctrl)
				initializer = mockterraformer.NewMockInitializer(ctrl)

				mgr = mockmanager.NewMockManager(ctrl)
				mgr.EXPECT().GetClient().Return(c)
				mgr.EXPECT().GetConfig().Return(&restConfig)
				mgr.EXPECT().GetScheme().Return(scheme).Times(2)

				actuator, err = NewActuatorWithDeps(
					mgr,
					alicloudClientFactory,
					terraformerFactory,
					terraformChartOps,
					nil,
					nil,
					false,
				)
				Expect(err).NotTo(HaveOccurred())

				cidr = "192.168.0.0/16"
				config = alicloudv1alpha1.InfrastructureConfig{
					Networks: alicloudv1alpha1.Networks{
						VPC: alicloudv1alpha1.VPC{
							CIDR: &cidr,
						},
					},
				}
				configYAML = expectEncode(runtime.Encode(serializer, &config))
				secretNamespace = "secretns"
				secretName = "secret"
				region = "region"
				infra = extensionsv1alpha1.Infrastructure{
					Spec: extensionsv1alpha1.InfrastructureSpec{
						DefaultSpec: extensionsv1alpha1.DefaultSpec{
							ProviderConfig: &runtime.RawExtension{
								Raw: configYAML,
							},
						},
						Region: region,
						SecretRef: corev1.SecretReference{
							Namespace: secretNamespace,
							Name:      secretName,
						},
					},
				}
				owner = metav1.NewControllerRef(&infra, extensionsv1alpha1.SchemeGroupVersion.WithKind(extensionsv1alpha1.InfrastructureResource))

				accessKeyID = "accessKeyID"
				accessKeySecret = "accessKeySecret"
				credentialsFile = "credentialsFile"

				podCIDR = "100.96.0.0/11"
				cluster = controller.Cluster{
					Shoot: &gardencorev1beta1.Shoot{
						Spec: gardencorev1beta1.ShootSpec{
							Region: region,
							Networking: &gardencorev1beta1.Networking{
								Pods: &podCIDR,
							},
						},
					},
				}
				initializerValues = InitializerValues{}
				chartValues = map[string]interface{}{
					"alicloud": map[string]interface{}{
						"region": "cn-shanghai-b",
					},
					"vpc": map[string]interface{}{
						"create": true,
						"id":     "alicloud_vpc.vpc.id",
						"cidr":   "10.10.10.10/6",
					},
					"natGateway": map[string]interface{}{
						"id":           "alicloud_nat_gateway.nat_gateway.id",
						"sNatTableIDs": "alicloud_nat_gateway.nat_gateway.snat_table_ids",
					},
					"eip": map[string]interface{}{
						"internetChargeType": "PayByTraffic",
					},
					"clusterName":  "test-namespace",
					"sshPublicKey": "PRIVATE_KEY",
					"zones": []map[string]interface{}{
						{
							"name": "cn-shanghai-b",
							"cidr": map[string]interface{}{
								"workers": "10.250.0.0/19",
							},
						},
					},
					"podCIDR": podCIDR,
					"outputKeys": map[string]interface{}{
						"vpcID":              "vpc_id",
						"vpcCIDR":            "vpc_cidr",
						"securityGroupID":    "sg_id",
						"vswitchNodesPrefix": "vswitch_z",
					},
				}

				vpcID = "vpcID"
				vpcCIDRString = "vpcCIDR"
				securityGroupID = "sgID"

				serviceLinkedRoleForNatGw = "AliyunServiceRoleForNatgw"
				serviceForNatGw = "nat.aliyuncs.com"

				fakeTfState = &realterraformer.RawState{
					Data: `
					{
						"resources": [
							{
							"mode": "managed",
							"type": "alicloud_eip",
							"instances": [
								{
									"attributes": {"ip_address": "139.196.40.2"}
								}
							]
							}
						]
					}
					`,
					Encoding: "none",
				}

			})

			It("should correctly reconcile the infrastructure", func() {
				describeNATGatewaysReq := vpc.CreateDescribeNatGatewaysRequest()
				describeNATGatewaysReq.VpcId = vpcID

				gomock.InOrder(
					c.EXPECT().Get(ctx, client.ObjectKey{Namespace: secretNamespace, Name: secretName}, gomock.AssignableToTypeOf(&corev1.Secret{})).
						SetArg(2, corev1.Secret{
							Data: map[string][]byte{
								alicloud.AccessKeyID:     []byte(accessKeyID),
								alicloud.AccessKeySecret: []byte(accessKeySecret),
								alicloud.CredentialsFile: []byte(credentialsFile),
							},
						}),

					alicloudClientFactory.EXPECT().NewRAMClient(region, accessKeyID, accessKeySecret).Return(shootRAMClient, nil),
					shootRAMClient.EXPECT().GetServiceLinkedRole(serviceLinkedRoleForNatGw).Return(nil, nil),
					shootRAMClient.EXPECT().CreateServiceLinkedRole(region, serviceForNatGw).Return(nil),

					terraformerFactory.EXPECT().NewForConfig(gomock.Any(), &restConfig, TerraformerPurpose, infra.Namespace, infra.Name, imagevector.TerraformerImage()).
						Return(terraformer, nil),

					terraformer.EXPECT().UseProjectedTokenMount(true).Return(terraformer),
					terraformer.EXPECT().SetLogLevel("info").Return(terraformer),
					terraformer.EXPECT().SetTerminationGracePeriodSeconds(int64(630)).Return(terraformer),
					terraformer.EXPECT().SetDeadlineCleaning(5*time.Minute).Return(terraformer),
					terraformer.EXPECT().SetDeadlinePod(15*time.Minute).Return(terraformer),
					terraformer.EXPECT().SetOwnerRef(owner).Return(terraformer),

					terraformer.EXPECT().SetEnvVars(gomock.Any()).Return(terraformer),

					alicloudClientFactory.EXPECT().NewVPCClient(region, accessKeyID, accessKeySecret).Return(vpcClient, nil),

					terraformer.EXPECT().GetStateOutputVariables(ctx, TerraformerOutputKeyVPCID).
						Return(map[string]string{
							TerraformerOutputKeyVPCID: vpcID,
						}, nil),

					vpcClient.EXPECT().FetchEIPInternetChargeType(context.TODO(), nil, vpcID).Return(alicloudclient.DefaultInternetChargeType, nil),

					terraformChartOps.EXPECT().ComputeCreateVPCInitializerValues(&config, alicloudclient.DefaultInternetChargeType).Return(&initializerValues),
					terraformChartOps.EXPECT().ComputeChartValues(&infra, &config, cluster.Shoot.Spec.Networking.Pods, &initializerValues).Return(chartValues),

					terraformerFactory.EXPECT().DefaultInitializer(c, gomock.Any(), gomock.Any(), gomock.Any(), gomock.AssignableToTypeOf(realterraformer.CreateState)).Return(initializer),

					terraformer.EXPECT().InitializeWith(ctx, initializer).Return(terraformer),

					terraformer.EXPECT().Apply(ctx),

					c.EXPECT().Get(ctx, client.ObjectKey{Namespace: secretNamespace, Name: secretName}, gomock.AssignableToTypeOf(&corev1.Secret{})).
						SetArg(2, corev1.Secret{
							Data: map[string][]byte{
								alicloud.AccessKeyID:     []byte(accessKeyID),
								alicloud.AccessKeySecret: []byte(accessKeySecret),
								alicloud.CredentialsFile: []byte(credentialsFile),
							},
						}),
					alicloudClientFactory.EXPECT().NewECSClient(region, accessKeyID, accessKeySecret).Return(shootECSClient, nil),
					alicloudClientFactory.EXPECT().NewROSClient(region, accessKeyID, accessKeySecret).Return(shootROSClient, nil),
					alicloudClientFactory.EXPECT().NewSTSClient(region, accessKeyID, accessKeySecret).Return(shootSTSClient, nil),
					shootSTSClient.EXPECT().GetAccountIDFromCallerIdentity(ctx).Return("", nil),

					terraformer.EXPECT().GetStateOutputVariables(ctx, TerraformerOutputKeyVPCID, TerraformerOutputKeyVPCCIDR, TerraformerOutputKeySecurityGroupID).
						Return(map[string]string{
							TerraformerOutputKeyVPCID:           vpcID,
							TerraformerOutputKeyVPCCIDR:         vpcCIDRString,
							TerraformerOutputKeySecurityGroupID: securityGroupID,
						}, nil),
					terraformer.EXPECT().GetRawState(ctx).Return(fakeTfState, nil),
					c.EXPECT().Status().Return(sw),
					sw.EXPECT().Patch(ctx, &infra, gomock.Any()),
				)
				Expect(actuator.Reconcile(ctx, logger, &infra, &cluster)).To(Succeed())
				Expect(infra.Status.ProviderStatus.Object).To(Equal(&alicloudv1alpha1.InfrastructureStatus{
					TypeMeta: StatusTypeMeta,
					VPC: alicloudv1alpha1.VPCStatus{
						ID: vpcID,
						SecurityGroups: []alicloudv1alpha1.SecurityGroup{
							{
								Purpose: alicloudv1alpha1.PurposeNodes,
								ID:      securityGroupID,
							},
						},
					},
				}))
				Expect(infra.Status.EgressCIDRs).To(Equal([]string{
					"139.196.40.2/32",
				}))
			})

			It("should correctly restore the infrastructure", func() {
				state := "some data"
				rawState = &realterraformer.RawState{
					Data:     "c29tZSBkYXRh",
					Encoding: "base64",
				}
				rawStateInBytes, _ := rawState.Marshal()
				infra.Status.State = &runtime.RawExtension{
					Raw: rawStateInBytes,
				}

				gomock.InOrder(
					c.EXPECT().Get(ctx, client.ObjectKey{Namespace: secretNamespace, Name: secretName}, gomock.AssignableToTypeOf(&corev1.Secret{})).
						SetArg(2, corev1.Secret{
							Data: map[string][]byte{
								alicloud.AccessKeyID:     []byte(accessKeyID),
								alicloud.AccessKeySecret: []byte(accessKeySecret),
								alicloud.CredentialsFile: []byte(credentialsFile),
							},
						}),

					alicloudClientFactory.EXPECT().NewRAMClient(region, accessKeyID, accessKeySecret).Return(shootRAMClient, nil),
					shootRAMClient.EXPECT().GetServiceLinkedRole(serviceLinkedRoleForNatGw).Return(nil, nil),
					shootRAMClient.EXPECT().CreateServiceLinkedRole(region, serviceForNatGw).Return(nil),

					terraformerFactory.EXPECT().NewForConfig(gomock.Any(), &restConfig, TerraformerPurpose, infra.Namespace, infra.Name, imagevector.TerraformerImage()).
						Return(terraformer, nil),

					terraformer.EXPECT().UseProjectedTokenMount(true).Return(terraformer),
					terraformer.EXPECT().SetLogLevel("info").Return(terraformer),
					terraformer.EXPECT().SetTerminationGracePeriodSeconds(int64(630)).Return(terraformer),
					terraformer.EXPECT().SetDeadlineCleaning(5*time.Minute).Return(terraformer),
					terraformer.EXPECT().SetDeadlinePod(15*time.Minute).Return(terraformer),
					terraformer.EXPECT().SetOwnerRef(owner).Return(terraformer),

					terraformer.EXPECT().SetEnvVars(gomock.Any()).Return(terraformer),

					alicloudClientFactory.EXPECT().NewVPCClient(region, accessKeyID, accessKeySecret).Return(vpcClient, nil),

					terraformer.EXPECT().GetStateOutputVariables(ctx, TerraformerOutputKeyVPCID).
						Return(map[string]string{
							TerraformerOutputKeyVPCID: vpcID,
						}, nil),
					vpcClient.EXPECT().FetchEIPInternetChargeType(context.TODO(), nil, vpcID).Return(alicloudclient.DefaultInternetChargeType, nil),

					terraformChartOps.EXPECT().ComputeCreateVPCInitializerValues(&config, alicloudclient.DefaultInternetChargeType).Return(&initializerValues),
					terraformChartOps.EXPECT().ComputeChartValues(&infra, &config, cluster.Shoot.Spec.Networking.Pods, &initializerValues).Return(chartValues),

					terraformerFactory.EXPECT().DefaultInitializer(c, gomock.Any(), gomock.Any(), gomock.Any(), realterraformer.CreateOrUpdateState{State: &state}).Return(initializer),

					terraformer.EXPECT().InitializeWith(ctx, initializer).Return(terraformer),

					terraformer.EXPECT().Apply(ctx),

					c.EXPECT().Get(ctx, client.ObjectKey{Namespace: secretNamespace, Name: secretName}, gomock.AssignableToTypeOf(&corev1.Secret{})).
						SetArg(2, corev1.Secret{
							Data: map[string][]byte{
								alicloud.AccessKeyID:     []byte(accessKeyID),
								alicloud.AccessKeySecret: []byte(accessKeySecret),
								alicloud.CredentialsFile: []byte(credentialsFile),
							},
						}),
					alicloudClientFactory.EXPECT().NewECSClient(region, accessKeyID, accessKeySecret).Return(shootECSClient, nil),
					alicloudClientFactory.EXPECT().NewROSClient(region, accessKeyID, accessKeySecret).Return(shootROSClient, nil),
					alicloudClientFactory.EXPECT().NewSTSClient(region, accessKeyID, accessKeySecret).Return(shootSTSClient, nil),
					shootSTSClient.EXPECT().GetAccountIDFromCallerIdentity(ctx).Return("", nil),

					terraformer.EXPECT().GetStateOutputVariables(ctx, TerraformerOutputKeyVPCID, TerraformerOutputKeyVPCCIDR, TerraformerOutputKeySecurityGroupID).
						Return(map[string]string{
							TerraformerOutputKeyVPCID:           vpcID,
							TerraformerOutputKeyVPCCIDR:         vpcCIDRString,
							TerraformerOutputKeySecurityGroupID: securityGroupID,
						}, nil),
					terraformer.EXPECT().GetRawState(ctx).Return(fakeTfState, nil),
					c.EXPECT().Status().Return(sw),
					sw.EXPECT().Patch(ctx, &infra, gomock.Any()),
				)

				Expect(actuator.Restore(ctx, logger, &infra, &cluster)).To(Succeed())
				Expect(infra.Status.ProviderStatus.Object).To(Equal(&alicloudv1alpha1.InfrastructureStatus{
					TypeMeta: StatusTypeMeta,
					VPC: alicloudv1alpha1.VPCStatus{
						ID: vpcID,
						SecurityGroups: []alicloudv1alpha1.SecurityGroup{
							{
								Purpose: alicloudv1alpha1.PurposeNodes,
								ID:      securityGroupID,
							},
						},
					},
				}))
				Expect(infra.Status.EgressCIDRs).To(Equal([]string{
					"139.196.40.2/32",
				}))
			})
		})
	})
})
