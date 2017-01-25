Gdlv is a graphical frontend to [Delve](https://github.com/derekparker/delve). Gdlv has been tested on Linux only at this moment but it should be easily ported to Windows and OS X.

[Demo video here](https://aarzilli.github.io/gdlv/doc/screencast.webm).

![Screenshot](https://raw.githubusercontent.com/aarzilli/gdlv/master/doc/screen.png)

# Setup

First make sure you have the current version of delve installed:
```
go get -u github.com/derekparker/delve/cmd/dlv
```
then install gdlv:
```
go get -u github.com/aarzilli/gdlv
```

# News

## 2016-01-25
* Implemented selective step into. Right click on a function call on the current line to step into that function call (note: not that function, that *function call*). Also accessible through the `step` command with `step -list`:

![Step Into](https://raw.githubusercontent.com/aarzilli/gdlv/master/doc/stepinto.png)

