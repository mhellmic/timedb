timedb
======
An alternative to `time` that saves its own history.


The unix time command is benchmarking for the poor. This adaptation makes the concept a bit more powerful by keeping a history of timed commands and allowing searches not only by the command string itself, but also by time ranges and runtime criteria, e.g. exit code, or system time used.

Installation
------------

```
go get github.com/mhellmic/timedb
go install github.com/mhellmic/timedb
```

Usage
-----

Time a command:
```
$ timedb du -sh /home
151G	.
	107.39 real	2.56 user	60.48 sys
```

Look it up later:
```
$ timedb -search  du
2016-07-01 10:08:44	du -sh /tmp/downloads	= 	0.02 real	0.00 user	0.01 sys
2016-07-01 10:38:53	du -sh /usr	= 	52.53 real	1.08 user	28.55 sys
2016-07-05 12:07:06	du -sh	= 	107.39 real	2.56 user	60.48 sys
```

Filter the search by time range:
```
$ timedb -search 2.7.2016- du
2016-07-05 12:07:06	du -sh	= 	107.39 real	2.56 user	60.48 sys
```

Filter by runtime duration:
```
$ timedb -search du 'Walltime>30s'
2016-07-01 10:38:53	du -sh /usr	= 	52.53 real	1.08 user	28.55 sys
2016-07-05 12:07:06	du -sh	= 	107.39 real	2.56 user	60.48 sys
```

Loop up all filter options:
```
$ timedb -keywordhelp
```

Contribute
----------

Feel free to send a pull request or fork
