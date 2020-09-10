This docker exporter focuses on things not usually provided by cAdvisor

Currently it exports:
- health status of running containers as an enumeration: basically a gauge with values corresponding to container states.
  - 0 - n/a
  - 1 - starting
  - 2 - healthy
  - -1 - unhealthy
  - -2 - unknown health check state
- momentary max (across processes in a container) open file descriptors, useful if you did set limits on the container and want to keep an eye on it  