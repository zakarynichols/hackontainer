# Hackontainer

_In Progress_

A fully compliant open container initiative runtime. 

### OCI runtime validation tests

Run all tests and write to single file:

__runc__
```bash
cd runtime-tools
sudo RUNTIME=runc find validation/ -name "*.t" -exec {} \; > ../validation_results.txt 2>&1
sudo cat ../validation_results_runc.txt
```

__hackontainer__
```bash
cd runtime-tools
sudo RUNTIME=../hackontainer find validation/ -name "*.t" -exec {} \; > ../validation_results.txt 2>&1
sudo cat ../validation_results_hackontainer.txt
```

See progress as it runs:

__runc__
```bash
cd runtime-tools
sudo RUNTIME=runc find validation/ -name "*.t" -exec {} \; 2>&1 | tee ../validation_results_runc.txt
```

__hackontainer__
```bash
cd runtime-tools
sudo RUNTIME=../hackontainer find validation/ -name "*.t" -exec {} \; 2>&1 | tee ../validation_results_hackontainer.txt
```