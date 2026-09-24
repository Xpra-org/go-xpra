# Changelog
## [0.2.3] 2026-09-24
* Platforms, build and packaging:
  * [fix the Debian golang installation](https://github.com/Xpra-org/go-xpra/commit/6c5db1b1889ec7b48626b228fe92f34502fb3732)
  * [add macOS support](https://github.com/Xpra-org/go-xpra/commit/268889d5f1ce8b2c98b7b2b346653e5f90351f5b)
  * [add the native macOS port](https://github.com/Xpra-org/go-xpra/commit/1395869b8bbd7a03accf2aa0fc410f3b0d91227e)
  * [add CycloneDX SBOMs to the release builds](https://github.com/Xpra-org/go-xpra/commit/71c4d7c60d3fb0b2ff405518941109c8cdec97e0)
* Features:
  * [add mmap screen update support](https://github.com/Xpra-org/go-xpra/commit/38459b387046128fe922395f79aa1e71201ba3cf)
  * [add bidirectional UTF-8 text clipboard synchronization on X11, Wayland and Windows](https://github.com/Xpra-org/go-xpra/commit/80c0fcbdeec949d0debbaebb3a754bae351bb763)
  * [add a Windows notification-area session icon with an Exit menu](https://github.com/Xpra-org/go-xpra/commit/8f63dba1a6da560c4d9272f3a60757292ee17bf4)
  * [avoid crashes when optional clipboard initialization fails](https://github.com/Xpra-org/go-xpra/commit/dcea4a827cc921a46e18e7012703faecb00f61d7)
  * [expose the desktop size in hello capabilities](https://github.com/Xpra-org/go-xpra/commit/3fafa310d0533e26e51bdfe91a006215f270c57f)
  * [expose per-monitor display capabilities](https://github.com/Xpra-org/go-xpra/commit/3d4a35d34d899efb914ce620a2a33ff366f39f2d)
  * [send monitor-relative event coordinates](https://github.com/Xpra-org/go-xpra/commit/3ee843ad08db6da2d1ad64a25cb11d76d7c0636d)
  * [handle SIGINT gracefully](https://github.com/Xpra-org/go-xpra/commit/f4dc6f40ff9de1013e09fc24d83e76d31424bd3a)
* Network:
  * [add Unix-domain socket support](https://github.com/Xpra-org/go-xpra/commit/7b6bef521f806360a59955f519831d160d96a6f3)
  * [advertise window forwarding in the "window" namespace](https://github.com/Xpra-org/go-xpra/commit/7d5e6b70811158a54f4789fbbba0b555e38fa4aa)
  * [send the correct disconnect packet type in (non-)backwards compatible mode](https://github.com/Xpra-org/go-xpra/commit/e8f6ca6f09c4f61bdd86e0f411a0a86453d0783d)
  * [flush the write queue before closing the connection](https://github.com/Xpra-org/go-xpra/commit/969d7166824f18da530137ed7d71da971ac2c822)
  * [don't send pings to servers that don't support them](https://github.com/Xpra-org/go-xpra/commit/7831df13e517958a5cbf4969bb9f4d613b563b15)
  * [expose the minimum protocol version in the hello](https://github.com/Xpra-org/go-xpra/commit/49398a330f4218b6c0cfbd359d311ebaa4c3df01)
  * [match upstream's "protocol-version" capability renaming](https://github.com/Xpra-org/go-xpra/commit/8b92bedfda4e20d69ca49ed5f9e110fa3db13847)
* Documentation and testing:
  * [mockserver: advertise the rencodeplus packet encoder](https://github.com/Xpra-org/go-xpra/commit/cf8ed22a53cd50c3108be8ceaeb1aa32984361b2)
  * [mockserver: send an empty file capability](https://github.com/Xpra-org/go-xpra/commit/60a753c9bb0e42e0a0309315971178ad82e9cc31)
  * [mockserver: accept the legacy client packet names](https://github.com/Xpra-org/go-xpra/commit/dd50f84a0533a0de27af03040e558bef10f9f632)
  * [mockserver: document using it with an xpra 6.5.x client](https://github.com/Xpra-org/go-xpra/commit/6f76fd363eb917c8025ce09966e28a39c4dbf2f0)

## [0.2.2] 2026-08-01
* Platforms, build and packaging:
  * [download source and verify its checksum](https://github.com/Xpra-org/go-xpra/commit/427a87a310e0f3fd80facb9d65f66014b2ecc6b0)
  * [our build scripts already use symlinks](https://github.com/Xpra-org/go-xpra/commit/519a0e78e30f90adba4ed2ad9cec6e141e8b660f)
  * [add the go-xpra manual page](https://github.com/Xpra-org/go-xpra/commit/2e8ef31bc76fcf691c2a0872ef70b2400b845944)
  * [make it easier to diagnose dependency issues](https://github.com/Xpra-org/go-xpra/commit/e4e38f598ae5da0a76e4213c8cbff8cc250cbbbb)
  * [lower the Go requirement to the real floor](https://github.com/Xpra-org/go-xpra/commit/cde34a6f367409f0f1a48ba67c7368d909ee466c)
  * [ask apt to install golang, but use a newer one if too old](https://github.com/Xpra-org/go-xpra/commit/4dcb8d42d2498f66adba8cdcc6238b28af27157e)
* Network:
  * [avoid killing a reaped SSH process](https://github.com/Xpra-org/go-xpra/commit/ebc5eeadcc4746175900b5a0adc2533238bc35f6)
  * [use wheel packets for scroll events](https://github.com/Xpra-org/go-xpra/commit/de82392e30634dbc3bdcde206305036fbe6f5ed7)
* Documentation and testing:
  * [add the project changelog](https://github.com/Xpra-org/go-xpra/commit/5bfb828f3f9617c20c9b1f0fb3e2833d31d91a81)
  * [add changelog entries](https://github.com/Xpra-org/go-xpra/commit/3ce49bb5d3ada18945948b8d6f731e14e21590f5)
  * [link to package downloads](https://github.com/Xpra-org/go-xpra/commit/a5d060cfaf51320e47079ce10c4f9f93e2681373)
  * [add the docs folder with the dependency graph](https://github.com/Xpra-org/go-xpra/commit/00291ce89cdc28522a4e63dbb9ee51dcefb225f4)
  * [publish the dependency graph on github pages](https://github.com/Xpra-org/go-xpra/commit/26ba25a54934bbbd7b90de17081365ec1ff74225)
## [0.2.1] 2026-07-30
* Platforms, build and packaging:
  * [bump the version](https://github.com/Xpra-org/go-xpra/commit/e7267422ee511408f3a57afabf2ec45e0645264f)
  * [share common Linux UI and keysym code](https://github.com/Xpra-org/go-xpra/commit/4fd18b76a10aaf1f15ba9bfe00cf5fb54f44af9b)
  * [add a native Wayland backend](https://github.com/Xpra-org/go-xpra/commit/2d90b56bfdedb3942415b29cf35c119f18a2363e)
  * [report when the Wayland compositor does not draw window frames](https://github.com/Xpra-org/go-xpra/commit/64f1cb68fc79c2accd81aa39abeb5a337fe59754)
  * [add an executable icon on MS Windows](https://github.com/Xpra-org/go-xpra/commit/931747fc328e91281afe3e612345889d0f06dc4e)
  * [add the Go-styled icon to the main page](https://github.com/Xpra-org/go-xpra/commit/4a1cc645704c1fc95fb53c03950348c0647b25a5)
  * [keep the initial MS Windows title bar accessible](https://github.com/Xpra-org/go-xpra/commit/fabb8c7201fa15dc46b163ba395ffcb1702e54a6)
  * [fix dialog icons on MS Windows](https://github.com/Xpra-org/go-xpra/commit/f016eb1b6b81cbadf67e6f790c90a115b487a012)
  * [remove duplicated asset functions](https://github.com/Xpra-org/go-xpra/commit/291f1bd740119a9611f71ff320d20628fd64cd9a)
  * [add RPM and Debian packaging](https://github.com/Xpra-org/go-xpra/commit/1a4a3a5ceca9568d321d944afc9faa8556663409)
  * [package the desktop launcher and icon](https://github.com/Xpra-org/go-xpra/commit/b6a9dbd30de4eae94957eb94beecddc7d1972a73)
* Network:
  * [add SSL support](https://github.com/Xpra-org/go-xpra/commit/03ff31ce567e95ab8fb8107dfe0fc25bac1ffff8)
  * [add the SSH subprocess transport](https://github.com/Xpra-org/go-xpra/commit/53545a0a30b35907efd6c1dd3267413729500766)
  * [use modern Xpra packet types](https://github.com/Xpra-org/go-xpra/commit/5fe012108bbb8999ac5fc99636882501946ba309)
  * [gate legacy packets behind the compatibility toggle](https://github.com/Xpra-org/go-xpra/commit/fb6666d799d6c463cf4be9cb84e1a81a7b025d33)
  * [gate the draw acknowledgement layout by compatibility mode](https://github.com/Xpra-org/go-xpra/commit/f93209253bced4fcbdbb1bbe40d9b9796e9d0f60)
  * [add WebSocket transports](https://github.com/Xpra-org/go-xpra/commit/4336200727ec76c344327cbf9e1da1523f447e1f)
  * [support out-of-band chunks](https://github.com/Xpra-org/go-xpra/commit/a8c186f63208c14171995561e6cc0dd9554cfd8c)
* Encodings:
  * [decode WebP using video-range YCbCr](https://github.com/Xpra-org/go-xpra/commit/1be43cdd612807e3d49937cfbb9d1d690d3fcc59)
* Features:
  * [add native authentication prompts](https://github.com/Xpra-org/go-xpra/commit/cd31433b5955fe98c9344e0aca322cf16259e05e)
  * [add a connection dialog for argument-free startup](https://github.com/Xpra-org/go-xpra/commit/c086cf29b9b83e0e51039fdeffb110f96a8e9155)
  * [add window icon support](https://github.com/Xpra-org/go-xpra/commit/3bb0b158fa9a16736ebeb47759705f9f2365140c)
  * [use the Xpra icon in dialogs](https://github.com/Xpra-org/go-xpra/commit/175c8e98ddc3115bb659a99de1536237cf6a9955)
* Documentation and testing:
  * [add the mock server and its test guide](https://github.com/Xpra-org/go-xpra/commit/229350907653689092bb7e0c8ae5a766d6dad713)
  * [complete the mock server connection support](https://github.com/Xpra-org/go-xpra/commit/35303e0d0f937ac03e6148b30a289ef8c31aaed4)
  * [document mock server testing](https://github.com/Xpra-org/go-xpra/commit/19c96fa5311e6ffe06783c57b5af75333583c4ca)
## [0.2.0] 2026-07-28
* Platforms, build and packaging:
  * [document building on MSYS2](https://github.com/Xpra-org/go-xpra/commit/5601e8a5a8d17aa3f19ed1fa6db55b25edec2f1f)
  * [ignore the MS Windows executable](https://github.com/Xpra-org/go-xpra/commit/502c32eca051cef7a9f87399881b90a90e607975)
  * [prevent CRLF problems](https://github.com/Xpra-org/go-xpra/commit/bb0a06b1ae6dea3a7ab025dd2e37b22b0cb59ea2)
* Encodings:
  * [add JPEG and PNG decoding](https://github.com/Xpra-org/go-xpra/commit/b401eaa5f8c29961fcdeb10b3d0ffeac88eaf775)
  * [add WebP decoding](https://github.com/Xpra-org/go-xpra/commit/a60360f43e742bae15a2324bfe0c1e0eae7bfe21)
* Features:
  * [log server lifecycle events](https://github.com/Xpra-org/go-xpra/commit/5d5f021897e106f534de10f60a6c6c6d86107ef4)
  * [handle window raise requests](https://github.com/Xpra-org/go-xpra/commit/25f6d431dbe2f4ba49772fa8e89953aa339d064b)
  * [add forwarded bell support](https://github.com/Xpra-org/go-xpra/commit/1eee7551fa07667bb8a95278eb45674a4a418959)
  * [log forwarded notifications](https://github.com/Xpra-org/go-xpra/commit/94439cce1a104faa6b4c73bd0813a548aafb5c92)
  * [handle show-desktop requests](https://github.com/Xpra-org/go-xpra/commit/1cf2b577847792306020f77f9d8a6309e178963b)
  * [apply server-provided cursors](https://github.com/Xpra-org/go-xpra/commit/00a06d4d510f58ae313745dfff20a765908e22f8)
## [0.1.1] 2026-07-28
* Platforms, build and packaging:
  * [compress release binaries with UPX](https://github.com/Xpra-org/go-xpra/commit/a7ad3259ede5c80273015a971e1758d5fc9881f1)
  * [add MS Windows support](https://github.com/Xpra-org/go-xpra/commit/c6d7062584a30df2d0c580bc3b5b88c032dfd283)
* Features:
  * [expose the version flag](https://github.com/Xpra-org/go-xpra/commit/a276940a0da39d9a079bc95281c63ce22099dd85)
* Network:
  * [parse standard Xpra TCP URLs](https://github.com/Xpra-org/go-xpra/commit/729983da14fca4ab1848efbc7dc40a39e4aa9228)
## [0.1.0] 2026-07-28
* Platforms, build and packaging:
  * [create the initial project](https://github.com/Xpra-org/go-xpra/commit/2182ea84b9b9f374210c527468453586496f1d41)
  * [add the initial client implementation](https://github.com/Xpra-org/go-xpra/commit/a559ad8c4b0d6300d46cace3518b8b288a621419)
  * [add CI tests and tagged release workflows](https://github.com/Xpra-org/go-xpra/commit/c9f095eef15d3e29fd6283ab29e0fe45f08f53ec)
