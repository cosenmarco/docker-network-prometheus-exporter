This docker exporter focuses on things not usually provided by cAdvisor

Currently it exports:
- health status of running containers as an enumeration
- momentary max (across processes in a container) open file descriptors, useful if you did set limits on the container and want to keep an eye on it  