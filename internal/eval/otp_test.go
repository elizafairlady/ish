package eval

import "testing"

// =====================================================================
// OTP: spawn, await, send, receive (basic — already passing)
// =====================================================================

func TestOTP_SpawnReturn(t *testing.T) {
	run(t, "pid = spawn fn do 42 end\necho $(await pid)", "42\n")
}

func TestOTP_SendReceivePattern(t *testing.T) {
	run(t, `pid = spawn fn do
receive do
  {:ping, sender} -> send sender, :pong
end
end
send pid, {:ping, self}
receive do
  :pong -> echo "pong"
end`, "pong\n")
}

// =====================================================================
// OTP: spawn_link — error propagation
// =====================================================================

func TestOTP_SpawnLinkCrashPropagate(t *testing.T) {
	run(t, `r = try do
pid = spawn_link fn do
  1 / 0
end
await pid
rescue
_ -> :caught
end
echo $r`, ":caught\n")
}

func TestOTP_SpawnLinkNormalExit(t *testing.T) {
	run(t, "pid = spawn_link fn do 99 end\necho $(await pid)", "99\n")
}

// =====================================================================
// OTP: monitor
// =====================================================================

func TestOTP_MonitorNormalExit(t *testing.T) {
	run(t, `pid = spawn fn do 42 end
monitor pid
receive do
  {:DOWN, _, :normal} -> echo "exited normally"
end`, "exited normally\n")
}

func TestOTP_MonitorDeadProcess(t *testing.T) {
	// Monitoring an already-dead process sends :DOWN immediately
	run(t, `pid = spawn fn do nil end
await pid
monitor pid
receive do
  {:DOWN, _, _} -> echo "got down"
end`, "got down\n")
}

// =====================================================================
// OTP: supervisor
// =====================================================================

func TestOTP_SupervisorRestart(t *testing.T) {
	run(t, `sup = supervise [
  fn do
    receive do
      :crash -> 1 / 0
    end
  end
], strategy: :one_for_one
echo "supervised"`, "supervised\n")
}

// =====================================================================
// OTP: receive with timeout
// =====================================================================

func TestOTP_ReceiveTimeout(t *testing.T) {
	run(t, `pid = spawn fn do
receive 100 do
  _ -> echo "got message"
after
  echo "timed out"
end
end
await pid`, "timed out\n")
}

// =====================================================================
// OTP: selective receive
// =====================================================================

func TestOTP_SelectiveReceive(t *testing.T) {
	// Requires savequeue: non-matching messages saved for later receives
	run(t, `pid = spawn fn do
receive do
  {:second, sender} -> send sender, :got_second
end
receive do
  {:first, sender} -> send sender, :got_first
end
end
send pid, {:first, self}
send pid, {:second, self}
receive do
  :got_second -> echo "second"
end
receive do
  :got_first -> echo "first"
end`, "second\nfirst\n")
}

// =====================================================================
// OTP: process linking chains
// =====================================================================

func TestOTP_LinkChain(t *testing.T) {
	// If A links to B and B crashes, A should get notified
	run(t, `r = try do
b = spawn_link fn do
  1 / 0
end
await b
rescue
_ -> :chain_caught
end
echo $r`, ":chain_caught\n")
}
