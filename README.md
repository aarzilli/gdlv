Gdlv is a graphical frontend to [Delve](https://github.com/derekparker/delve) for Linux, Windows and macOS.

[Demo video here](https://aarzilli.github.io/gdlv/doc/screencast.webm).

![Screenshot](https://raw.githubusercontent.com/aarzilli/gdlv/master/doc/screen.png)

![Gdlv on macOS](https://raw.githubusercontent.com/aarzilli/gdlv/master/doc/sierra-screen.png)

# Setup

First make sure you have the current version of delve installed:
```
go get -u github.com/go-delve/delve/cmd/dlv
```
then install gdlv:
```
go get -u github.com/aarzilli/gdlv
```

Use Ctrl+plus and Ctrl+minus to change font size.

# News

## 2019-06-07
* Added Starlark as a scripting language

## 2019-05-11
* Start from main.main instead of runtime.main.
* Better interface when concurrent breakpoints happen during next, step or stepout.

## 2018-12-14
* Deferred calls view.
* Limit number of goroutines that are loaded (improves performance when debugging programs with tens of thousands of goroutines while the goroutines window is opened).
* Make pastel theme the default theme.
* Fixed bug handling disabled breakpoints.

## 2018-11-23
* Fix build on go1.9
* New, extended, syntax for pinning expressions `dp @f/somefunc/ a` will evaluate `a` in the first frame calling `somefunc`.

## 2018-10-29
* [Pastel theme](https://raw.githubusercontent.com/aarzilli/gdlv/master/doc/screen-pastel.png)

## 2018-09-26
* Breakpoint persistence
* pretty printing of time.Time variables
* Notification for truncated stack traces
* Ability to have an expression's value printed to the command window every time a continue command completes.

## 2018-08-26
* Fixed some race conditions
* Redesigned detail views, they are now updated while stepping through the code

## 2018-08-16
* Highlight variable names
* Expose starting location of goroutines
* Miscellaneous bug fixes

## 2018-07-02
* Support font changes
* Sort variables by declaration line
* Miscellaneous bug fixes

## 2018-06-13
* Print return values when stepping out of a function
* Allow setting breakpoints after the program has exited

## 2018-05-21
* Implemented path substitution rules

## 2018-02-12
* Implemented "Continue to line"
* Let `restart` change program arguments
* Made load parameters configurable

## 2017-12-20
* Support for upcoming go 1.10.
* Changed how split windows are implemented (floating windows with docking).

## 2017-09-17
* "Find Element" command: search through a slice or an array for the element matching a given expression.
* New red theme.
* Only recompile if one of the source files changed.
* `step -last` command option to step into the last call on the line.
* Search command history with Ctrl+R

## 2017-06-29
* Pinning of expressions to specific execution frames.
* Keybindings for continue, next, step and stepout
* Compact visualization for interface values

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

