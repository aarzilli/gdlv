Gdlv is a graphical frontend to [Delve](https://github.com/derekparker/delve) for Linux, Windows and macOS.

[Demo video here](https://aarzilli.github.io/gdlv/doc/screencast.webm).

![Screenshot](https://raw.githubusercontent.com/aarzilli/gdlv/master/doc/screen.png)

![Gdlv on macOS](https://raw.githubusercontent.com/aarzilli/gdlv/master/doc/sierra-screen.png)

# Setup

First make sure you have the current version of delve installed:
```
go get -u github.com/derekparker/delve/cmd/dlv
```
then install gdlv:
```
go get -u github.com/aarzilli/gdlv
```

Use Ctrl+plus and Ctrl+minus to change font size.

# News

## 2017-06-04
* Custom formatters for user defined types.
* Better executable building for go1.9

## 2017-05-18
* Better formatting for maps and integer variables.

## 2017-05-07
* Added core command
* Support for multiple backends
* Added "replay" startup command, "checkpoint" command and "Checkpoints" view.

## 2017-03-01
* Horizontal scrollbars for all panels

## 2017-02-09
* Goroutines panel will show a breakpoint icon for goroutines stopped at a breakpoint.

## 2017-02-06
* Implemented selective step into. Right click on a function call on the current line to step into that function call (note: not that function, that *function call*). Also accessible through the `step` command with `step -list`:

![Step Into](https://raw.githubusercontent.com/aarzilli/gdlv/master/doc/stepinto.png)

