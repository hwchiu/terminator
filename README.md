Terminator [![Build Status](https://travis-ci.org/hwchiu/terminator.svg?branch=master)](https://travis-ci.org/hwchiu/terminator) [![Docker Build Status](https://img.shields.io/docker/build/hwchiu/terminator.svg)](https://hub.docker.com/r/hwchiu/terminator/)
==========
A sidecar container for fluentd in kubernetes job.

Introduction
============
In some cases, you will use sidecar container in your kubernetes jobs 
to monitor the resource or something but the sidecar container won't exit even the main 
container has exited.

The terminator is userful for your kubernetes jobs, the terminator stop the sidecar container
after the main container has exited.

How to use
==========
In your kubernetes job, you should deploy three containers, which including
1. main container
2. sidecar container
3. terminator

For the terminator, you should pass three environment when you run the terminator
- POD_NAMESPACE: the kubernetes namespace which your jobs runs, the default is `default` namespace.
- POD_NAME: the pod name of your kubernetes jobs.
- TARGET_NAME: the container name of your main application.

How it works
============
The terminator will watch the events of kubernetes jobs and it will send a http request to sidecar container to
stop it once the main continaer exit.

It support the fluuentd now hence it will send the GET http request to `http://127.0.0.1:/24444/api/processes.interruptWorkers`
