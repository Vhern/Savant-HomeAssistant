-- A made-up serviceImplementation.sqlite with the same tables as a Savant 12 Mac host.
CREATE TABLE Zones (id INTEGER PRIMARY KEY, name TEXT, type TEXT, serviceID TEXT, logicalComponent TEXT, componentID TEXT, guid TEXT);
CREATE TABLE LightEntities (id INTEGER PRIMARY KEY, zoneID INTEGER, name TEXT, addresses TEXT, entityType TEXT, entitySubType TEXT, technology TEXT, pressCommand TEXT, holdCommand TEXT, releaseCommand TEXT, togglePressCommand TEXT, toggleHoldCommand TEXT, toggleReleaseCommand TEXT, dimmerCommand TEXT, fadeTime INTEGER, delayTime INTEGER, stateName TEXT, lightsAreOn INTEGER, roomLightsControl INTEGER, orderValue INTEGER, isSceneable INTEGER, naturalLightCapable INTEGER, trueImageSupport INTEGER, bleNetworkKey TEXT, bleGroupID TEXT, bleNodeID TEXT, isSavantLoad INTEGER);
CREATE TABLE LightProperties (id INTEGER PRIMARY KEY, lightingEntityID INTEGER, maxKelvinTemp INTEGER, minKelvinTemp INTEGER);
CREATE TABLE ShadeEntities (id INTEGER PRIMARY KEY, zoneID INTEGER, name TEXT, addresses TEXT, entityType TEXT, pressCommand TEXT, holdCommand TEXT, releaseCommand TEXT, togglePressCommand TEXT, toggleHoldCommand TEXT, toggleReleaseCommand TEXT, dimmerCommand TEXT, fadeTime INTEGER, delayTime INTEGER, presetNumber INTEGER, stateName TEXT, sceneNumber INTEGER, orderValue INTEGER, isSceneable INTEGER);
CREATE TABLE DoorLockEntities (id INTEGER PRIMARY KEY, zoneID INTEGER, name TEXT, entityType TEXT, addresses TEXT, lockCommand TEXT, unlockCommand TEXT, lockState TEXT, errorState TEXT, supportsStatus INTEGER, supportsErrors INTEGER, camera TEXT, partner TEXT, authentication TEXT);
CREATE TABLE ServiceImplementationZonedService (id INTEGER PRIMARY KEY, zone TEXT, component TEXT, logicalComponent TEXT, serviceType TEXT, service TEXT, serviceVariantID TEXT, shareMode TEXT, externalZone TEXT, nameOnComponent TEXT, alias TEXT, avioType TEXT);
CREATE TABLE ServiceImplementationRequests (id INTEGER PRIMARY KEY, request TEXT, arguments TEXT, initialState TEXT, finalState TEXT, isCustom INTEGER, isInterService INTEGER);
CREATE TABLE ServiceImplementationRequestMap (id INTEGER PRIMARY KEY, ServiceImplementationZonedService_id INTEGER, ServiceImplementationRequests_id INTEGER);
CREATE TABLE ServiceImplementationServiceResources (id INTEGER PRIMARY KEY, zone TEXT, component TEXT, logicalComponent TEXT, serviceType TEXT, service TEXT, serviceVariantID TEXT, shareMode TEXT, externalZone TEXT, nameOnComponent TEXT, connectorID TEXT, alias TEXT, avioType TEXT, resourceType TEXT, pathOrder INTEGER, avType TEXT, slotID TEXT, moduleName TEXT, mediaType TEXT, logicalIO TEXT, discreteVolume INTEGER, serviceNameAlias TEXT);

INSERT INTO Zones (id, name, type) VALUES (1, 'Den', 'Environmental'), (2, 'Den', 'Environmental'), (3, 'Den', 'User');

INSERT INTO LightEntities (id, zoneID, name, addresses, entityType, entitySubType, technology, pressCommand, releaseCommand, toggleReleaseCommand, dimmerCommand, stateName, orderValue) VALUES
  (1, 1, 'Cove', '001,4,(null),(null),(null),(null)', 'DMX', 'RGBWW', 'Infinite Color', 'DimmerSet', '', '', 'DimmerSet', 'Test Host.Lighting.CurrentColor_4_001', 0),
  (2, 1, 'Lamp', '002,17,/zone/57,(null),(null),(null)', 'DMX', 'RGBW', 'Infinite Color', 'DimmerSet', '', '', 'DimmerSet', 'Test Host.Lighting.CurrentColor_17_002', 1),
  (3, 1, 'Pendants', '002,2,/zone/43,(null),(null),(null)', 'Dimmer', 'W', 'Classic White', 'DimmerSet', '', '', 'DimmerSet', 'Test Host.Lighting.CurrentDimmerLevel_2_002', 2),
  (4, 1, 'Cove Scene', 'Cove Scene,(null),(null),(null),(null),(null)', 'Scene', '', '', '', 'ActivateScene', 'DeactivateScene', '', 'Test Host.Lighting.IsSceneActive_Cove Scene', 3),
  (5, 1, 'Away', '002,1025,/button/37,(null),(null),(null)', 'Scene', '', '', 'ButtonPressAndRelease', '', '', '', 'Test Host.Lighting.CurrentLEDState_1025_002', 4),
  (6, 1, '1', '002,1026,/button/36,(null),(null),(null)', 'Scene', '', '', 'ButtonPressAndRelease', '', '', '', 'Test Host.Lighting.CurrentLEDState_1026_002', 5),
  (7, 1, 'Away', '004,6,(null),(null),(null),(null)', 'Scene', '', '', 'ButtonPress', 'ButtonRelease', 'ButtonRelease', '', 'Test Host.Lighting.CurrentLEDState_6_004', 6);
INSERT INTO LightProperties (lightingEntityID, maxKelvinTemp, minKelvinTemp) VALUES (1, 6000, 2200);

INSERT INTO ShadeEntities (id, zoneID, name, addresses, entityType, pressCommand, releaseCommand, dimmerCommand, stateName, orderValue) VALUES
  (1, 2, 'Sheers', '002,1,/zone/13,(null),(null)', 'Variable', '', 'GroupShadeSet', '', 'Test Host.Lighting.GroupShadeLevel_1_002', 0);

INSERT INTO ServiceImplementationZonedService (id, zone, component, logicalComponent, serviceType, serviceVariantID, alias) VALUES
  (1, 'Den', 'Test Host', 'Lighting', 'SVC_ENV_LIGHTING', '1', 'Test Host'),
  (2, 'Den', 'Test Host', 'Lighting', 'SVC_ENV_SHADE', '1', 'Test Host'),
  (3, 'Den', 'Den Apple TV', 'Media_server', 'SVC_AV_APPLEREMOTEMEDIASERVER', '1', 'Apple TV');

INSERT INTO ServiceImplementationRequests (id, request, arguments, isCustom, isInterService) VALUES
  (1, 'DimmerSet', 'Address1,Address2,Address3,Address4,Address5,Address6,DimmerLevel,FadeTime,DelayTime,bleColorRed,bleColorGreen,bleColorBlue,bleColorWhite,kelvin,Curve', 0, 0),
  (2, 'ActivateScene', 'Address1,Address2,Address3,Address4,Address5,Address6', 0, 0),
  (3, 'DeactivateScene', 'Address1,Address2,Address3,Address4,Address5,Address6', 0, 0),
  (4, 'ButtonPressAndRelease', 'Address1,Address2,Address3,Address4,Address5,Address6', 0, 0),
  (5, 'ButtonPress', 'Address1,Address2,Address3,Address4,Address5,Address6', 0, 0),
  (6, 'ButtonRelease', 'Address1,Address2,Address3,Address4,Address5,Address6', 0, 0),
  (7, '__RoomLightsOff', '', 0, 0),
  (10, 'GroupShadeUp', 'Address1,Address2,Address3,Address4,Address5,Address6', 0, 0),
  (11, 'GroupShadeDown', 'Address1,Address2,Address3,Address4,Address5,Address6', 0, 0),
  (12, 'GroupShadeStop', 'Address1,Address2,Address3,Address4,Address5,Address6', 0, 0),
  (13, 'GroupShadeSet', 'Address1,Address2,Address3,Address4,Address5,Address6,ShadeLevel,FadeTime,DelayTime', 0, 0),
  (14, 'ShadeStop', 'Address1,Address2,Address3,Address4,Address5', 0, 0),
  (15, 'ShadeSet', 'Address1,Address2,Address3,Address4,Address5,Address6,ShadeLevel,FadeTime,DelayTime', 0, 0),
  (20, 'PowerOn', '', 0, 0), (21, 'PowerOff', '', 0, 0), (22, 'Play', '', 0, 0), (23, 'Pause', '', 0, 0),
  (24, 'VolumeUp', '', 0, 0), (25, 'VolumeDown', '', 0, 0), (26, 'MuteOn', '', 0, 0), (27, 'MuteOff', '', 0, 0),
  (28, 'SetVolume', 'VolumeValue', 0, 0), (29, 'Menu', '', 0, 0), (30, 'Home', '', 0, 0),
  (31, 'OSDCursorUp', '', 0, 0), (32, 'OSDCursorDown', '', 0, 0), (33, 'OSDSelect', '', 0, 0),
  (34, 'LaunchApp', 'AppID', 0, 0), (35, 'Den TV_PowerToggle', '', 0, 0),
  (36, 'Netflix', '', 1, 0), (37, 'Youtube', '', 1, 0), (38, 'Apple TV Restart', '', 1, 0),
  (39, 'PowerOffTV', '', 0, 1);

INSERT INTO ServiceImplementationRequestMap (ServiceImplementationZonedService_id, ServiceImplementationRequests_id)
  SELECT 1, id FROM ServiceImplementationRequests WHERE id < 10
  UNION ALL SELECT 2, id FROM ServiceImplementationRequests WHERE id BETWEEN 10 AND 19
  UNION ALL SELECT 3, id FROM ServiceImplementationRequests WHERE id >= 20;

CREATE TABLE CameraEntities (id INTEGER PRIMARY KEY, zoneID INTEGER, name TEXT, componentName TEXT, logicalComponent TEXT, entityType TEXT, previewPath TEXT, fullscreenPath TEXT, previewURL TEXT, fullscreenURL TEXT, highSourceURL TEXT, lowSourceURL TEXT, highMulticastURL TEXT, lowMulticastURL TEXT, previewFormat TEXT, fullscreenFormat TEXT, hostname TEXT, username TEXT, password TEXT, cpassword TEXT, authentication TEXT, port TEXT, transport TEXT, partner TEXT, uriSource TEXT);
INSERT INTO Zones (id, name, type) VALUES (9, 'SAVANT_GLOBAL_ENV_ZONE', 'Environmental');
INSERT INTO CameraEntities (id, zoneID, name, componentName, logicalComponent, entityType, previewURL, fullscreenURL, highSourceURL, lowSourceURL, username, password) VALUES
  (1, 9, 'Porch Camera', 'Porch Camera', 'Security_camera1', 'Fixed', 'http://viewer:secret@10.9.9.2/cgi-bin/snapshot.cgi', 'http://viewer:secret@10.9.9.2/cgi-bin/snapshot.cgi', 'rtsp://viewer:secret@10.9.9.2/cam/realmonitor?channel=1&subtype=0', 'rtsp://viewer:secret@10.9.9.2/cam/realmonitor?channel=1&subtype=1', 'viewer', 'secret'),
  (2, 9, 'Blank Camera', 'Blank Camera', 'Security_camera1', 'Fixed', '', '', '', '', '', '');
