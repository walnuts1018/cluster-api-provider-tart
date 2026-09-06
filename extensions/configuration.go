package extensions

import (
	"context"
	"time"

	clusterv1 "sigs.k8s.io/cluster-api/api/core/v1beta2"
	"sigs.k8s.io/controller-runtime/pkg/client"

	bootstrapv1alpha1 "github.com/walnuts1018/cluster-api-provider-tart/api/bootstrap/v1alpha1"
	"github.com/walnuts1018/cluster-api-provider-tart/talos"
	"github.com/walnuts1018/cluster-api-provider-tart/update"
)

// updateTalosNodeは、machine configuration updateが必要とするTalos APIの観測と操作だけを表す。
// 実機のTalos APIを必要とする経路をここへ閉じ込め、policy部分をGo testから検証できるようにする。
type updateTalosNode interface {
	// ActiveMachineConfigurationは稼働中nodeへ現在適用されているmachine configurationを観測する。
	ActiveMachineConfiguration(ctx context.Context) ([]byte, error)
	// ApplyConfigurationLiveはrebootなしでmachine configurationを適用する。
	ApplyConfigurationLive(ctx context.Context, configuration []byte) error
	// ApplyConfigurationはmachine configurationを適用する。rebootを要する差分は次のrebootで反映される。
	ApplyConfiguration(ctx context.Context, configuration []byte) error
	// RebootはTalos nodeのgraceful rebootを要求する。
	Reboot(ctx context.Context) error
	// BootTimeは稼働中nodeのboot時刻を観測する。値の変化はrebootが完了したことの観測根拠になる。
	BootTime(ctx context.Context) (uint64, error)
	// ServicesHealthyはTalosが管理するserviceのhealthを確認する。
	ServicesHealthy(ctx context.Context) error
}

// configurationUpdateはmachine configuration差分の適用に必要な観測と副作用をまとめる。
type configurationUpdate struct {
	node   updateTalosNode
	policy bootstrapv1alpha1.ConfigurationUpdatePolicy
	// desiredはimmutable Bootstrap Secretが保持するdesired complete machine configurationである。
	desired []byte
	// rebootGateはreboot前に満たすべき安全条件(control planeのetcd quorum、cordon/drain)を評価する。
	// falseを返した場合はrebootへ進まず、返したメッセージでretryする。
	rebootGate func(ctx context.Context) (bool, string)
	// nodeReadyはworkload cluster上のNodeがReadyであることを観測する。Nodeが未参加の場合はtrueを返す。
	nodeReady func(ctx context.Context) (bool, string)
	// rebootObservationTimeoutはreboot要求後にboot時刻の変化を観測するために待つ上限である。
	rebootObservationTimeout time.Duration
	// rebootObservationIntervalはboot時刻を再観測する間隔である。
	rebootObservationInterval time.Duration
}

// configurationUpdateOutcomeはmachine configuration updateの結果を表す。Statusをprogram counterとして保存しないため、
// 呼び出しごとにTalosとworkload clusterの観測から再計算できる粗い結果だけを返す。
type configurationUpdateOutcome struct {
	// doneはdesired configurationの反映とnodeの回復まで確認できたことを示す。
	done bool
	// retryMessageが空でない場合、外部の観測が整うまで待って再試行する。
	retryMessage string
	// failureMessageが空でない場合、安全に継続できないため停止する。Machine replacementへfallbackしない。
	failureMessage string
}

const (
	defaultRebootObservationTimeout  = 90 * time.Second
	defaultRebootObservationInterval = 5 * time.Second
)

// applyConfigurationUpdateは、active configurationとdesired configurationの差分をpolicyへ従って適用し、
// 適用後の反映と回復を観測する。RPCの成功だけでは完了とみなさない。
func applyConfigurationUpdate(ctx context.Context, updater configurationUpdate) configurationUpdateOutcome {
	if updater.node == nil || len(updater.desired) == 0 {
		return configurationUpdateOutcome{failureMessage: "The desired machine configuration is unavailable; the in-place update is stopped."}
	}
	active, err := updater.node.ActiveMachineConfiguration(ctx)
	if err != nil {
		return configurationUpdateOutcome{retryMessage: "The active Talos machine configuration could not be observed while the in-place update is being prepared."}
	}
	decision, err := update.Evaluate(updater.policy, active, updater.desired)
	if err != nil {
		return configurationUpdateOutcome{failureMessage: "The machine configuration difference could not be evaluated safely; the in-place update is stopped."}
	}
	switch decision.Class {
	case update.ChangeInvariantConflict:
		return configurationUpdateOutcome{failureMessage: decision.Reason}
	case update.ChangeReprovisionRequired:
		return configurationUpdateOutcome{failureMessage: "ReprovisionRequired: " + decision.Reason}
	case update.ChangeNone:
		return verifyConfigurationRecovered(ctx, updater)
	case update.ChangeUpdatable:
		// policyに従って適用するため、この関数の後半で扱う。
	}
	if decision.ApplyMode == update.ApplyModeLive {
		if err := updater.node.ApplyConfigurationLive(ctx, updater.desired); err != nil {
			// Live policyはユーザーがreboot-freeの適用を明示した場合だけ使う。失敗をrebootで隠さず明示的に停止する。
			return configurationUpdateOutcome{failureMessage: "The Live machine configuration apply failed; the update is stopped without falling back to a reboot."}
		}
		return configurationUpdateOutcome{retryMessage: "The machine configuration was applied without a reboot; waiting for the node to report the desired configuration."}
	}
	if updater.rebootGate != nil {
		proceed, message := updater.rebootGate(ctx)
		if !proceed {
			return configurationUpdateOutcome{retryMessage: message}
		}
	}
	bootTime, bootTimeErr := updater.node.BootTime(ctx)
	if err := updater.node.ApplyConfiguration(ctx, updater.desired); err != nil {
		return configurationUpdateOutcome{failureMessage: "The Talos API rejected the machine configuration apply; the Machine remains stopped for safety."}
	}
	if err := updater.node.Reboot(ctx); err != nil {
		return configurationUpdateOutcome{failureMessage: "The Talos API rejected the reboot after the machine configuration apply; the Machine remains stopped for safety."}
	}
	if bootTimeErr == nil {
		if observed := observeReboot(ctx, updater, bootTime); !observed {
			return configurationUpdateOutcome{retryMessage: "The machine configuration was applied; waiting for the node to reboot into the desired configuration."}
		}
	}
	return configurationUpdateOutcome{retryMessage: "The machine configuration was applied and the node rebooted; waiting for the desired configuration and Node readiness to be observed."}
}

// observeRebootは、reboot要求前に観測したboot時刻が変化することを確認する。API接続が失われている間はerrorになるため、
// 上限時間まで再観測を続ける。観測できなかった場合はfalseを返し、次回の呼び出しで改めて観測し直す。
func observeReboot(ctx context.Context, updater configurationUpdate, previousBootTime uint64) bool {
	timeout := updater.rebootObservationTimeout
	if timeout <= 0 {
		timeout = defaultRebootObservationTimeout
	}
	interval := updater.rebootObservationInterval
	if interval <= 0 {
		interval = defaultRebootObservationInterval
	}
	deadline := time.Now().Add(timeout)
	for {
		if bootTime, err := updater.node.BootTime(ctx); err == nil && bootTime != previousBootTime {
			return true
		}
		if time.Now().After(deadline) {
			return false
		}
		select {
		case <-ctx.Done():
			return false
		case <-time.After(interval):
		}
	}
}

// verifyConfigurationRecoveredは、desired configurationが反映済みのnodeがTalosとKubernetesの双方で回復したことを確認する。
func verifyConfigurationRecovered(ctx context.Context, updater configurationUpdate) configurationUpdateOutcome {
	if err := updater.node.ServicesHealthy(ctx); err != nil {
		return configurationUpdateOutcome{retryMessage: "The Talos services are not healthy yet after the machine configuration update."}
	}
	if updater.nodeReady != nil {
		ready, message := updater.nodeReady(ctx)
		if !ready {
			return configurationUpdateOutcome{retryMessage: message}
		}
	}
	return configurationUpdateOutcome{done: true}
}

// machineConfigurationUpdateは、UpdateMachineの観測結果からmachine configuration updateの実行contextを組み立てる。
// rebootを伴う適用では、control planeのetcd quorum判定とworkload Nodeのcordon/drainを同じ安全条件として要求する。
func machineConfigurationUpdate(kubeClient client.Reader, machine *clusterv1.Machine, preparation *machineUpdatePreparation, node *talos.Client) configurationUpdate {
	providerID := string(preparation.providerMachine.Spec.ProviderID)
	return configurationUpdate{
		node:    node,
		policy:  preparation.policy,
		desired: preparation.configuration,
		rebootGate: func(ctx context.Context) (bool, string) {
			if isControlPlaneMachine(machine) {
				gateContext, cancel := context.WithTimeout(ctx, talosUpdateTimeout)
				gateErr := controlPlaneUpgradeSafe(gateContext, kubeClient, machine, node)
				cancel()
				if gateErr != nil {
					return false, "The control-plane etcd quorum could not be proven safe for a Talos restart; waiting before the machine configuration reboot."
				}
			}
			return enforceDrainPolicy(ctx, kubeClient, machine, providerID)
		},
		nodeReady: func(ctx context.Context) (bool, string) {
			return nodeReadyForMachine(ctx, kubeClient, machine, providerID)
		},
	}
}

// bootstrapUpdatePolicyは、CAPI MachineがreferenceするTartBootstrapConfigのconfiguration update policyを返す。
func bootstrapUpdatePolicy(config *bootstrapv1alpha1.TartBootstrapConfig) bootstrapv1alpha1.ConfigurationUpdatePolicy {
	if config == nil {
		return bootstrapv1alpha1.ConfigurationUpdatePolicyAuto
	}
	return config.Spec.EffectiveConfigurationUpdatePolicy()
}

var _ updateTalosNode = (*talos.Client)(nil)
