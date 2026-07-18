# ディレクトリ構成

pkg/やinternal/は使わず、直下からディレクトリを切っていく
hackやscriptsもなるべく使わない。
kubebuilderが特定のディレクトリを前提としている場合はそれに従う

```
- api
- config
- cmd
    - hoge
        - main.go
    - fuga
        - main.go
- domain
    - <具体的なcontextの名前>
        - entity
        - errors
        - workflow
            - do_hoge
                - command.go
                - errors.go
                - workflow.go
        - steps
            - do_hoge
                - errors.go
                - step.go
- event_manager
- infrastructure
    - repository
        - hoge
            - repository.go
            - repository_test.go
    - service
        - hoge
            - service.go
            - service_test.go
    - k8s_controller
    - http_server
- ...
- test
- utils
    - hoge
    - fuga
    - testutils
- docs
```
