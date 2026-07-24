{
					__sveltekit_1oo58s9 = {
						base: ""
					};

					const element = document.currentScript.parentElement;

					Promise.all([
						import("/_app/immutable/entry/start.D94Ejenz.js"),
						import("/_app/immutable/entry/app.Dzt5fJeJ.js")
					]).then(([kit, app]) => {
						kit.start(app, element);
					});
				}
			