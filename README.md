# fluent-bit-clickhouse
Fluent-Bit go clickhouse output plugin, for Istio with love


Added custom parser for envoy logs. It extracts following fields:
- time
- method
- path
- code
- x_request_id

Credentials are moved to secret file, previously it was in pure envs. You should create secret with name {}. Example:
```yaml
apiVersion: v1
kind: Secret
metadata:
  name: clickhouse-logs-credentials
type: Opaque
data:
  CLICKHOUSE_USER: some_user
  CLICKHOUSE_PASSWORD: some_password
  CLICKHOUSE_HOST: localhost:9000
```


We could've implemented it just with config file, but the initial format of log is json and it will be passed to 
json parser without additional processing.  

In better world we could write like this, for example:
```
[PARSER]
  Name envoy
  Format regex
  Regex \[(?<time>[^\]]*)\] "(?<method>\S+)(?: +(?<path>[^\"]*?)(?: +\S*)?)(?: +?<protocol>\S+)?" (?<code>[^ ]*) (?<response_flags>[^ ]*) (?<response_code_details>[^ ]*) (?<connection_termination_details>[^ ]*) "(?<upstream_transport_failure_reason>[^ ]*)" (?<bytes_received>[^ ]*) (?<bytes_sent>[^ ]*) (?<duration>[^ ]*) (?<x-envoy-upstream-service-time>[^ ]*) "(?<x-forwarded-for>[^ ]*)" "(?<user-agent>[^\"]*)" "(?<x-request-id>[^ ]*)" "(?<authority>[^ ]*)" "(?<upstream_host>[^ ]*)" (?<upstream_cluster>[^ ]*) (?<upstream_local_address>[^ ]*) (?<downstream_local_address>[^ ]*) (?<downstream_remote_address>[^ ]*) (?<requested_server_name>[^ ]*) (?<route_name>[^ ]*)
  Time_Key time
  Time_Format %d/%b/%Y:%H:%M:%S %z
  Time_Keep   On
```

Istio envoy log sample:
```
[2022-05-10T13:40:13.396Z] "GET / HTTP/1.1" 200 - via_upstream - "-" 0 643 0 0 "10.0.128.1" "python-requests/2.27.1" "397105f8-18b7-44d9-9465-60fed294e453" "51.250.72.130" "10.0.128.42:80" inbound|80|| 127.0.0.6:52601 10.0.128.42:80 10.0.128.1:0 outbound_.80_._.sa-frontend.default.svc.cluster.local default
```