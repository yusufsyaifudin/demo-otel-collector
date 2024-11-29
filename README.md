# OPENTELEMETRY TRACES AND METRICS

This repository gives you simple example on how you can move from Datadog traces and metrics (using statsd) with fully OpenTelemetry SDK.
The OpenTelemetry collector agent should be installed as the replacement of Datadog Agent.


## Run Only Application

First, prepare the `DATADOG_AGENT_HOST` and `OTEL_EXPORTER_OTLP_ENDPOINT` in the environment variable.

Then, run the application.

```shell
DATADOG_AGENT_HOST=xxx DATADOG_AGENT_HOST=yyy docker-compose -f docker-compose-app-only.yaml up --build --force-recreate
````

## Run All Docker Containers

```shell
MY_POD_IP=$(ipconfig getifaddr en0) \
  NODE_NAME=my-local-k8s-node \
  DD_API_KEY=xxx \
  DOCKER_USER="$(id -u):$(id -g)" \
  PROMETHEUS_WRITE_ENDPOINT="http://grafana-mimir.example.com/api/v1/push" docker-compose up --build --force-recreate
```

What is the environment used for?

* `MY_POD_IP` is the IP of the pod where the application is running.
* `NODE_NAME` is the name of the node where the application is running. This is used to identify the node in the traces.
* `DD_API_KEY` is the Datadog API key. This is used to send traces and metrics to Datadog.
* `DOCKER_USER` is the user and group that should be used to run the application. This is used to avoid permission issues when writing to the log file.
* `PROMETHEUS_WRITE_ENDPOINT` is the Prometheus Write endpoint (or Pushgateway) where the metrics should be sent. This is used to send metrics to Prometheus.

All of these environment is not needed by the application, but only for Datadog Agent and OpenTelemetry Colletor Agent.


