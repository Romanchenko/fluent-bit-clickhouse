# fluent-bit-clickhouse
Fluent-Bit go clickhouse output plugin, for istio with love


Fixed parser for envoy logs
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