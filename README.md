# tea

A line oriented text processor and signaling tool.

It is in early stages of development.

## What is tea?

```bash
tea COMMAND [COMMAND...] -- PROGRAM [ARG...]
```

`tea` is a line oriented program. You can use it to start PROGRAM, and process its output line by line. You can specify
filter conditions and commands that can alter the output of PROGRAM, and also send signals and input data
to PROGRAM conditionally.

`tea` can be compared to the `tee` unix command. Instead of an arbitrary input stream, it works on the output of the 
process that is  started by tea itself, making it much easier to send conditional signals to the process.

Usage example: wait until "operation completed" appears in the output of a program, then send SIGINT

```bash
tea -c -p "operation completed" -s SIGINT -- command
```

Detailed documentation is in `cmd/tea/USAGE.txt`  (or just do `tea --help`)

Please note that time based conditions are not yet implemented. They are a planned feature.

